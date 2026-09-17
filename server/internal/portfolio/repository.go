package portfolio

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	clientevents "market-lens/server/internal/events"
	"market-lens/server/internal/instruments"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository owns every owner-scoped query and the single transactional write a change performs.
//
// One rule runs through every method below: the caller's user identifier is a required argument and
// appears in the WHERE clause of every statement. It is never inferred from a portfolio identifier,
// because a query scoped only by portfolio would return the right rows for the wrong person the
// first time an identifier leaked — and that is a breach, not a bug.
type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) ready() error {
	if r == nil || r.pool == nil {
		return errors.New("portfolio repository is not configured")
	}
	return nil
}

// Portfolio reads one person's, or reports that they have none yet. Having no portfolio is not an
// error: it is what everybody starts with.
func (r *Repository) Portfolio(ctx context.Context, userID string) (Portfolio, error) {
	if err := r.ready(); err != nil {
		return Portfolio{}, err
	}
	var held Portfolio
	if err := r.pool.QueryRow(ctx, `SELECT id::text, user_id::text, accounting_currency, created_at, updated_at
		FROM portfolios WHERE user_id = $1`, userID).Scan(
		(*string)(&held.ID), (*string)(&held.UserID), &held.AccountingCurrency,
		&held.CreatedAt, &held.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Portfolio{}, ErrNotFound
		}
		return Portfolio{}, fmt.Errorf("read the portfolio: %w", err)
	}
	return held, nil
}

// EnsurePortfolio creates one on first use and states its accounting currency.
func (r *Repository) EnsurePortfolio(ctx context.Context, userID, currency string) (Portfolio, error) {
	if err := r.ready(); err != nil {
		return Portfolio{}, err
	}
	id, err := instruments.NewUUID()
	if err != nil {
		return Portfolio{}, err
	}
	if _, err := r.pool.Exec(ctx, `INSERT INTO portfolios (id, user_id, accounting_currency)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET accounting_currency = excluded.accounting_currency,
		  updated_at = now()`, id.String(), userID, currency); err != nil {
		return Portfolio{}, fmt.Errorf("create the portfolio: %w", err)
	}
	return r.Portfolio(ctx, userID)
}

const tradeColumns = `t.id::text, t.portfolio_id::text, t.user_id::text, t.instrument_id::text,
	i.ticker, i.name, i.currency, t.direction, t.quantity::text, t.price::text, t.costs::text,
	t.trade_date::text, t.sequence, t.superseded_by::text, t.withdrawn_at, t.recorded_at, t.changed_at`

func scanTrade(row interface{ Scan(...any) error }) (Trade, error) {
	var trade Trade
	var superseded *string
	var withdrawn *time.Time
	if err := row.Scan((*string)(&trade.ID), (*string)(&trade.PortfolioID), (*string)(&trade.UserID),
		(*string)(&trade.InstrumentID), &trade.Ticker, &trade.Name, &trade.Currency,
		(*string)(&trade.Direction), &trade.Quantity, &trade.Price, &trade.Costs,
		(*string)(&trade.TradeDate), &trade.Sequence, &superseded, &withdrawn,
		&trade.RecordedAt, &trade.ChangedAt); err != nil {
		return Trade{}, err
	}
	switch {
	case withdrawn != nil:
		trade.Status = TradeWithdrawn
	case superseded != nil:
		trade.Status = TradeSuperseded
	default:
		trade.Status = TradeCurrent
	}
	return trade, nil
}

// CurrentTrades reads the versions that count, in the order first-in-first-out consumes them.
func (r *Repository) CurrentTrades(ctx context.Context, userID string) ([]Trade, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT `+tradeColumns+`
		FROM portfolio_trades t JOIN instruments i ON i.id = t.instrument_id
		WHERE t.user_id = $1 AND t.superseded_by IS NULL AND t.withdrawn_at IS NULL
		ORDER BY t.instrument_id::text, t.trade_date, t.sequence`, userID)
	if err != nil {
		return nil, fmt.Errorf("read the trades: %w", err)
	}
	defer rows.Close()
	var trades []Trade
	for rows.Next() {
		trade, err := scanTrade(rows)
		if err != nil {
			return nil, err
		}
		trades = append(trades, trade)
	}
	return trades, rows.Err()
}

// Trade reads one, scoped to its owner. A trade belonging to somebody else answers exactly as one
// that does not exist.
func (r *Repository) Trade(ctx context.Context, userID, tradeID string) (Trade, error) {
	if err := r.ready(); err != nil {
		return Trade{}, err
	}
	row := r.pool.QueryRow(ctx, `SELECT `+tradeColumns+`
		FROM portfolio_trades t JOIN instruments i ON i.id = t.instrument_id
		WHERE t.user_id = $1 AND t.id = $2`, userID, tradeID)
	trade, err := scanTrade(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Trade{}, ErrNotFound
		}
		return Trade{}, fmt.Errorf("read the trade: %w", err)
	}
	return trade, nil
}

// ListTrades pages the history newest first, keyset over (trade_date, sequence).
func (r *Repository) ListTrades(ctx context.Context, userID string, query TradeQuery) (TradePage, error) {
	if err := r.ready(); err != nil {
		return TradePage{}, err
	}
	limit := query.Limit
	if limit < 1 || limit > 200 {
		limit = 50
	}

	var page TradePage
	condition := ` AND t.withdrawn_at IS NULL`
	if query.IncludeWithdrawn {
		condition = ``
	}
	if query.Cursor == "" {
		var total int64
		if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM portfolio_trades t
			WHERE t.user_id = $1`+condition, userID).Scan(&total); err != nil {
			return TradePage{}, fmt.Errorf("count the trades: %w", err)
		}
		page.Total = &total
	}
	after, err := decodeCursor(query.Cursor)
	if err != nil {
		return TradePage{}, err
	}

	statement := `SELECT ` + tradeColumns + `
		FROM portfolio_trades t JOIN instruments i ON i.id = t.instrument_id
		WHERE t.user_id = $1` + condition
	args := []any{userID}
	if after != nil {
		statement += ` AND (t.trade_date, t.sequence) < ($2::date, $3::bigint)`
		args = append(args, after.date, after.sequence)
	}
	statement += ` ORDER BY t.trade_date DESC, t.sequence DESC LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, limit+1)

	rows, err := r.pool.Query(ctx, statement, args...)
	if err != nil {
		return TradePage{}, fmt.Errorf("list the trades: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		trade, err := scanTrade(rows)
		if err != nil {
			return TradePage{}, err
		}
		page.Items = append(page.Items, trade)
	}
	if err := rows.Err(); err != nil {
		return TradePage{}, err
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.Items = page.Items[:limit]
		page.NextCursor = encodeCursor(last.TradeDate.String(), last.Sequence)
	}
	return page, nil
}

type cursor struct {
	date     string
	sequence int64
}

func encodeCursor(date string, sequence int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(date + "|" + strconv.FormatInt(sequence, 10)))
}

func decodeCursor(value string) (*cursor, error) {
	if value == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, errors.New("the cursor is not readable")
	}
	date, rest, found := strings.Cut(string(raw), "|")
	if !found {
		return nil, errors.New("the cursor is not readable")
	}
	sequence, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return nil, errors.New("the cursor is not readable")
	}
	return &cursor{date: date, sequence: sequence}, nil
}

// WriteTrade records, corrects or withdraws in one transaction, publishing the owner-scoped event
// on the same one so a reader cannot be told about a change that is not there.
type writeRequest struct {
	PortfolioID UUID
	UserID      string
	// Insert is the new version, empty on a withdrawal.
	Insert *Trade
	// Supersede is the version being replaced, empty on a plain record.
	Supersede string
	// Withdraw marks a version withdrawn instead of superseding it.
	Withdraw string
}

func (r *Repository) WriteTrade(ctx context.Context, request writeRequest) (Trade, error) {
	if err := r.ready(); err != nil {
		return Trade{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Trade{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var written Trade
	if request.Insert != nil {
		trade := *request.Insert
		var sequence int64
		if err := tx.QueryRow(ctx, `SELECT coalesce(max(sequence), 0) + 1
			FROM portfolio_trades WHERE portfolio_id = $1`, request.PortfolioID.String()).
			Scan(&sequence); err != nil {
			return Trade{}, fmt.Errorf("allocate the sequence: %w", err)
		}
		// A correction keeps the sequence of the version it replaces: the person is fixing what a
		// trade said, not when they told us about it, and first-in-first-out consumes by that
		// order.
		if request.Supersede != "" {
			if err := tx.QueryRow(ctx, `SELECT sequence FROM portfolio_trades
				WHERE id = $1 AND user_id = $2`, request.Supersede, request.UserID).
				Scan(&sequence); err != nil {
				return Trade{}, fmt.Errorf("read the superseded sequence: %w", err)
			}
			// Supersede *before* inserting, so the two versions are never both live against the
			// partial unique index on the sequence. The self-reference is deferred to commit time,
			// which is what lets this point at a row that does not exist yet.
			tag, err := tx.Exec(ctx, `UPDATE portfolio_trades SET superseded_by = $3
				WHERE id = $1 AND user_id = $2 AND superseded_by IS NULL AND withdrawn_at IS NULL`,
				request.Supersede, request.UserID, trade.ID.String())
			if err != nil {
				return Trade{}, fmt.Errorf("supersede the trade: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return Trade{}, ErrNotFound
			}
		}
		trade.Sequence = sequence

		var changedAt any
		if request.Supersede != "" {
			changedAt = time.Now().UTC()
		}
		if _, err := tx.Exec(ctx, `INSERT INTO portfolio_trades
			(id, portfolio_id, user_id, instrument_id, direction, quantity, price, costs,
			 trade_date, sequence, changed_at)
			VALUES ($1,$2,$3,$4,$5,$6::numeric,$7::numeric,$8::numeric,$9::date,$10,$11)`,
			trade.ID.String(), request.PortfolioID.String(), request.UserID,
			trade.InstrumentID.String(), string(trade.Direction), trade.Quantity, trade.Price,
			trade.Costs, trade.TradeDate.String(), trade.Sequence, changedAt); err != nil {
			return Trade{}, fmt.Errorf("record the trade: %w", err)
		}
		written = trade
	}

	if request.Withdraw != "" {
		tag, err := tx.Exec(ctx, `UPDATE portfolio_trades SET withdrawn_at = now(), changed_at = now()
			WHERE id = $1 AND user_id = $2 AND superseded_by IS NULL AND withdrawn_at IS NULL`,
			request.Withdraw, request.UserID)
		if err != nil {
			return Trade{}, fmt.Errorf("withdraw the trade: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return Trade{}, ErrNotFound
		}
	}

	payload, err := json.Marshal(map[string]any{
		"portfolio_id": request.PortfolioID.String(),
		"change":       describeChange(request),
	})
	if err != nil {
		return Trade{}, fmt.Errorf("encode the change: %w", err)
	}
	// Scope 'user' with the subject set: the replay query filters on exactly this, and a
	// deactivated account is refused even with a cursor it held while active. The payload carries
	// what changed, never the figures — those are re-read through the authorized path.
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: EventChanged, Version: 1, Scope: "user", SubjectUserID: request.UserID,
		EntityType: "portfolio", EntityID: request.PortfolioID.String(),
		Payload: payload, OccurredAt: time.Now().UTC(),
	}); err != nil {
		return Trade{}, err
	}
	return written, tx.Commit(ctx)
}

func describeChange(request writeRequest) string {
	switch {
	case request.Withdraw != "":
		return "withdrawn"
	case request.Supersede != "":
		return "corrected"
	default:
		return "recorded"
	}
}

// InstrumentIsCarried reports whether this product has the instrument at all.
//
// The universe is the vocabulary. Somebody holding something outside it cannot record it here, and
// the refusal says so rather than failing to find something — a holding with no stored prices could
// never be valued, compared or explained.
func (r *Repository) InstrumentIsCarried(ctx context.Context, instrumentID string) (bool, error) {
	if err := r.ready(); err != nil {
		return false, err
	}
	var carried bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM instruments WHERE id = $1 AND active)`, instrumentID).Scan(&carried); err != nil {
		return false, fmt.Errorf("read the instrument: %w", err)
	}
	return carried, nil
}

// CurrencyIsConvertible reports whether this product could value anything in a currency.
//
// A currency is usable when it is the euro — every stored rate converts from it directly — or when
// a rate quoting it against the euro exists. Accepting one with neither would leave every holding
// permanently unvalued with the person unable to see why.
func (r *Repository) CurrencyIsConvertible(ctx context.Context, currency string) (bool, error) {
	if err := r.ready(); err != nil {
		return false, err
	}
	if currency == "EUR" {
		return true, nil
	}
	var known bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM fx_rates WHERE base = 'EUR' AND quote = $1)`, currency).Scan(&known); err != nil {
		return false, fmt.Errorf("read the rates for %s: %w", currency, err)
	}
	return known, nil
}

// latestPrices reads each instrument's most recent stored close, and the session it came from.
//
// Per instrument rather than one session for the whole portfolio: a portfolio-wide session would
// leave Helsinki unvalued whenever Stockholm traded more recently, which is the union-calendar
// problem feature 021 already solved this way. Stating the session per holding is what makes "this
// figure is two days old" visible rather than hidden inside a total.
func (r *Repository) latestPrices(ctx context.Context, instrumentIDs []string) (map[UUID]priced, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	prices := map[UUID]priced{}
	if len(instrumentIDs) == 0 {
		return prices, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT ON (instrument_id)
		instrument_id::text, session_date::text, close::text
		FROM daily_price_bars WHERE instrument_id = ANY($1::uuid[])
		ORDER BY instrument_id, session_date DESC`, instrumentIDs)
	if err != nil {
		return nil, fmt.Errorf("read the latest prices: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, session, close string
		if err := rows.Scan(&id, &session, &close); err != nil {
			return nil, err
		}
		value, err := parseDec(close)
		if err != nil {
			return nil, fmt.Errorf("close for %s on %s: %w", id, session, err)
		}
		prices[UUID(id)] = priced{close: value, session: SessionDate(session)}
	}
	return prices, rows.Err()
}

// ratesOn reads the euro rates for one session. Both legs of a cross must come from the same
// session, so they are read together rather than looked up one at a time.
func (r *Repository) ratesOn(ctx context.Context, session SessionDate) (rates, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	quoted := rates{}
	rows, err := r.pool.Query(ctx, `SELECT quote, rate::text FROM fx_rates
		WHERE base = 'EUR' AND session_date = $1::date`, session.String())
	if err != nil {
		return nil, fmt.Errorf("read the rates for %s: %w", session, err)
	}
	defer rows.Close()
	for rows.Next() {
		var quote, rate string
		if err := rows.Scan(&quote, &rate); err != nil {
			return nil, err
		}
		value, err := parseDec(rate)
		if err != nil {
			return nil, fmt.Errorf("rate EUR/%s on %s: %w", quote, session, err)
		}
		quoted[quote] = value
	}
	return quoted, rows.Err()
}
