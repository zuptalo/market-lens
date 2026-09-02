package instruments

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The listing query answers the universe list in one statement. Everything it reports is
// derived from tables feature 002 already owns; this feature stores nothing of its own.
//
// Two properties are load-bearing and easy to lose in a later edit:
//
//   - A statistic that cannot be computed is NULL, never 0. Twenty stored sessions is one
//     short of the twenty-one a 20-session return needs, and answering 0 there would be a
//     claim the data does not support (FR-007).
//   - Sorting and paging happen here, over the whole result set, never over the page already
//     fetched (FR-005).

// sortExpression maps a sort key to the SQL it orders by and the type its cursor value casts
// back to. The map is the whitelist: a key that is not in it is rejected rather than
// interpolated, so no caller can reach the ORDER BY clause with arbitrary text.
type sortExpression struct {
	sql  string
	cast string
}

var listingSorts = map[ListingSort]sortExpression{
	SortName:          {"i.name", "text"},
	SortTicker:        {"i.ticker", "text"},
	SortExchange:      {"e.mic", "text"},
	SortSector:        {"coalesce(i.sector, '')", "text"},
	SortCountry:       {"i.country", "text"},
	SortLatestClose:   {"s.latest_close", "numeric"},
	SortChangePercent: {"s.change_percent", "float8"},
	SortReturn20:      {"s.return_20", "numeric"},
	SortReturn90:      {"s.return_90", "numeric"},
	SortVolatility:    {"s.volatility", "numeric"},
	// Freshness orders by how far behind an instrument is. An instrument with no history has
	// nothing to be behind, so it sorts last rather than pretending to be current.
	SortFreshness: {"s.sessions_behind", "int"},
}

// SupportedListingSort reports whether a sort key is one the contract defines, so the
// transport boundary can answer 400 without reaching the database.
func SupportedListingSort(sort ListingSort) bool {
	_, ok := listingSorts[sort]
	return ok
}

var errInvalidListingCursor = errors.New("invalid instrument listing cursor")

type listingCursor struct {
	Value      *string `json:"v"`
	ID         string  `json:"id"`
	Sort       string  `json:"s"`
	Descending bool    `json:"d"`
}

func encodeListingCursor(cursor listingCursor) string {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeListingCursor(raw string, filter ListingFilter) (*listingCursor, error) {
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errInvalidListingCursor
	}
	var cursor listingCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return nil, errInvalidListingCursor
	}
	if _, err := ParseUUID(cursor.ID); err != nil {
		return nil, errInvalidListingCursor
	}
	// A cursor carries the ordering it was produced under. Resuming it under a different one
	// would silently skip and repeat rows, so it is refused instead.
	if cursor.Sort != string(filter.Sort) || cursor.Descending != filter.Descending {
		return nil, errInvalidListingCursor
	}
	return &cursor, nil
}

// listingStatisticsCTE gathers each instrument's price facts from its stored bars and its
// three adopted statistics from the feature engine (feature 013, FR-026).
//
// The window function ranks each instrument's sessions most-recent-first, so "the previous
// close" means the second stored bar rather than yesterday's date. The statistics are not
// derived here at all: return_20, return_90 and volatility_20 are read from feature_values at
// the instrument's own latest stored session, so the number on the Markets list and the
// number the features endpoint serves are the same row of the same table. An instrument the
// engine has not computed shows them absent — never a zero, and never a second opinion
// derived from the same bars by different arithmetic.
const listingStatisticsCTE = `
adopted AS (
	SELECT d.id, d.name FROM feature_definitions d
	WHERE d.name IN ('return_20', 'return_90', 'volatility_20') AND d.superseded_at IS NULL
), bars AS (
	SELECT b.instrument_id, b.session_date, b.close,
	       row_number() OVER (PARTITION BY b.instrument_id ORDER BY b.session_date DESC) AS rn,
	       count(*) OVER (PARTITION BY b.instrument_id) AS stored_sessions
	FROM daily_price_bars b
	JOIN candidates c ON c.id = b.instrument_id
), aggregated AS (
	SELECT instrument_id,
	       max(stored_sessions) AS stored_sessions,
	       max(session_date) FILTER (WHERE rn = 1) AS latest_session,
	       max(close) FILTER (WHERE rn = 1) AS latest_close,
	       max(close) FILTER (WHERE rn = 2) AS previous_close
	FROM bars GROUP BY instrument_id
), engine AS (
	SELECT v.instrument_id,
	       max(v.value) FILTER (WHERE a.name = 'return_20') AS return_20,
	       max(v.value) FILTER (WHERE a.name = 'return_90') AS return_90,
	       max(v.value) FILTER (WHERE a.name = 'volatility_20') AS volatility
	FROM feature_values v
	JOIN adopted a ON a.id = v.definition_id
	JOIN aggregated g ON g.instrument_id = v.instrument_id AND g.latest_session = v.session_date
	GROUP BY v.instrument_id
), s AS (
	SELECT c.id AS instrument_id,
	       coalesce(a.stored_sessions, 0) AS stored_sessions,
	       a.latest_session,
	       a.latest_close,
	       a.previous_close,
	       a.latest_close - a.previous_close AS change_absolute,
	       CASE WHEN a.previous_close IS NOT NULL AND a.previous_close <> 0
	            THEN (a.latest_close / a.previous_close - 1)::float8 END AS change_percent,
	       f.return_20,
	       f.return_90,
	       f.volatility,
	       CASE WHEN a.latest_session IS NULL THEN NULL ELSE (
	           SELECT count(*) FROM exchange_sessions es
	           WHERE es.exchange_id = c.exchange_id
	             AND es.status IN ('open', 'half_day')
	             AND es.session_date > a.latest_session
	             AND es.session_date <= $1::date
	       ) END AS sessions_behind
	FROM candidates c
	LEFT JOIN aggregated a ON a.instrument_id = c.id
	LEFT JOIN engine f ON f.instrument_id = c.id
)`

func (r *Repository) Listing(ctx context.Context, filter ListingFilter) (ListingPage, error) {
	if filter.Limit < 1 || filter.Limit > 200 {
		return ListingPage{}, fmt.Errorf("%w: limit must be between 1 and 200", ErrInvalidQuery)
	}
	if filter.Sort == "" {
		filter.Sort = SortName
	}
	sort, ok := listingSorts[filter.Sort]
	if !ok {
		return ListingPage{}, fmt.Errorf("%w: unsupported sort %q", ErrInvalidQuery, filter.Sort)
	}
	if filter.AsOf == "" {
		return ListingPage{}, fmt.Errorf("%w: an as-of date is required", ErrInvalidQuery)
	}
	cursor, err := decodeListingCursor(filter.Cursor, filter)
	if err != nil {
		return ListingPage{}, fmt.Errorf("%w: %s", ErrInvalidQuery, err)
	}

	arguments := []any{filter.AsOf.String()}
	add := func(value any) string {
		arguments = append(arguments, value)
		return fmt.Sprintf("$%d", len(arguments))
	}

	conditions := []string{}
	if filter.ID != "" {
		conditions = append(conditions, "i.id = "+add(filter.ID.String())+"::uuid")
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		placeholder := add("%" + strings.ToLower(query) + "%")
		conditions = append(conditions, fmt.Sprintf(
			"(lower(i.ticker) LIKE %s OR lower(i.name) LIKE %s OR lower(i.isin) LIKE %s)",
			placeholder, placeholder, placeholder))
	}
	if filter.MIC != "" {
		conditions = append(conditions, "e.mic = "+add(filter.MIC))
	}
	if filter.Country != "" {
		conditions = append(conditions, "i.country = "+add(filter.Country))
	}
	if filter.Currency != "" {
		conditions = append(conditions, "i.currency = "+add(filter.Currency))
	}
	if filter.Sector != "" {
		conditions = append(conditions, "i.sector = "+add(filter.Sector))
	}
	switch filter.Status {
	case "":
	case "active":
		conditions = append(conditions, "i.active")
	case "inactive":
		conditions = append(conditions, "NOT i.active")
	default:
		return ListingPage{}, fmt.Errorf("%w: unsupported status %q", ErrInvalidQuery, filter.Status)
	}
	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	direction, comparison := "ASC", ">"
	if filter.Descending {
		direction, comparison = "DESC", "<"
	}
	// The instrument identifier follows the sort direction rather than staying ascending, so
	// that reversing the order really does reverse the result and a cursor never straddles a
	// tie. NULLS LAST in both directions keeps "we could not compute this" at the end, where
	// it reads as an absence rather than as an extreme value.
	ordering := fmt.Sprintf("ORDER BY %s %s NULLS LAST, i.id %s", sort.sql, direction, direction)

	keyset := ""
	if cursor != nil {
		id := add(cursor.ID)
		if cursor.Value == nil {
			// Already inside the trailing block of rows whose sort value is absent; only the
			// identifier can move us forward.
			keyset = fmt.Sprintf("AND %s IS NULL AND i.id %s %s::uuid", sort.sql, comparison, id)
		} else {
			value := add(*cursor.Value)
			keyset = fmt.Sprintf(
				"AND (%s IS NULL OR (%s, i.id) %s (%s::%s, %s::uuid))",
				sort.sql, sort.sql, comparison, value, sort.cast, id)
		}
	}

	limit := add(filter.Limit + 1)
	statement := fmt.Sprintf(`WITH candidates AS (
	SELECT i.id, i.exchange_id FROM instruments i
	JOIN exchanges e ON e.id = i.exchange_id
	%s
), %s
SELECT i.id::text, i.exchange_id::text, i.isin, i.ticker, i.name, i.currency, i.country,
       i.instrument_type, coalesce(i.sector, ''), coalesce(i.industry, ''), i.active,
       i.purchasability_status, i.created_at, i.updated_at,
       e.mic, e.name, e.country, e.currency, e.timezone, e.active,
       s.latest_session::text, s.latest_close::text, s.previous_close::text,
       s.change_absolute::text, s.change_percent,
       s.return_20::text, s.return_90::text, s.volatility::text,
       s.stored_sessions, s.sessions_behind,
       (%s)::text AS sort_value
FROM instruments i
JOIN exchanges e ON e.id = i.exchange_id
JOIN s ON s.instrument_id = i.id
%s %s
%s
LIMIT %s`, where, listingStatisticsCTE, sort.sql, where, keyset, ordering, limit)

	// The size of the filtered set, counted only when the reader is at the start of it. On a
	// later page the client already holds the number, and counting again would make the page
	// materialise every matching row — the opposite of what keyset paging is for (R-001). The
	// count reuses the same WHERE construction as the page, so the two cannot disagree.
	var total *int64
	if cursor == nil {
		// The filter's placeholders keep the numbering they were built with, so $1 — the as-of
		// date, which only the freshness subquery uses — has to be mentioned for PostgreSQL to
		// type it. Naming it here is cheaper and less error-prone than renumbering the filter.
		countConditions := strings.Replace(where, "WHERE ", "AND ", 1)
		countStatement := fmt.Sprintf(`SELECT count(*) FROM instruments i
			JOIN exchanges e ON e.id = i.exchange_id
			WHERE $1::date IS NOT NULL %s`, countConditions)
		var counted int64
		if err := r.pool.QueryRow(ctx, countStatement, arguments[:len(arguments)-1]...).Scan(&counted); err != nil {
			return ListingPage{}, fmt.Errorf("count instruments: %w", err)
		}
		total = &counted
	}

	rows, err := r.pool.Query(ctx, statement, arguments...)
	if err != nil {
		return ListingPage{}, fmt.Errorf("list instruments: %w", err)
	}
	defer rows.Close()

	items := make([]ListingRow, 0, filter.Limit)
	sortValues := make([]*string, 0, filter.Limit)
	for rows.Next() {
		row, sortValue, err := scanListingRow(rows)
		if err != nil {
			return ListingPage{}, err
		}
		items = append(items, row)
		sortValues = append(sortValues, sortValue)
	}
	if err := rows.Err(); err != nil {
		return ListingPage{}, fmt.Errorf("list instruments: %w", err)
	}

	next := ""
	if len(items) > filter.Limit {
		last := items[filter.Limit-1]
		next = encodeListingCursor(listingCursor{
			Value: sortValues[filter.Limit-1], ID: last.ID.String(),
			Sort: string(filter.Sort), Descending: filter.Descending,
		})
		items = items[:filter.Limit]
	}
	return ListingPage{Items: items, NextCursor: next, Total: total}, nil
}

func scanListingRow(rows pgx.Rows) (ListingRow, *string, error) {
	var row ListingRow
	var id, exchangeID, mic, exchangeName, exchangeCountry, exchangeCurrency, timezone string
	var exchangeActive bool
	var latestSession, latestClose, previousClose, changeAbsolute, sortValue *string
	var return20, return90, volatility *string
	var sessionsBehind *int

	if err := rows.Scan(&id, &exchangeID, &row.ISIN, &row.Ticker, &row.Name, &row.Currency,
		&row.Country, &row.Type, &row.Sector, &row.Industry, &row.Active,
		&row.PurchasabilityStatus, &row.CreatedAt, &row.UpdatedAt,
		&mic, &exchangeName, &exchangeCountry, &exchangeCurrency, &timezone, &exchangeActive,
		&latestSession, &latestClose, &previousClose, &changeAbsolute,
		&row.ChangePercent, &return20, &return90, &volatility,
		&row.StoredSessions, &sessionsBehind, &sortValue); err != nil {
		return ListingRow{}, nil, fmt.Errorf("scan instrument listing row: %w", err)
	}

	row.ID = UUID(id)
	row.ExchangeID = UUID(exchangeID)
	row.Exchange = Exchange{ID: UUID(exchangeID), MIC: mic, Name: exchangeName,
		Country: exchangeCountry, Currency: exchangeCurrency, Timezone: timezone, Active: exchangeActive}
	if latestSession != nil {
		row.LatestSession = SessionDate(*latestSession)
	}
	row.LatestClose = decimalPointer(latestClose)
	row.PreviousClose = decimalPointer(previousClose)
	row.ChangeAbsolute = decimalPointer(changeAbsolute)
	row.Return20 = decimalPointer(return20)
	row.Return90 = decimalPointer(return90)
	row.Volatility = decimalPointer(volatility)

	// Freshness is a fact about the exchange calendar, not about the clock: an instrument is
	// current when it has a bar for the most recent session its own exchange was open for.
	switch {
	case latestSession == nil:
		row.Freshness = Freshness{State: FreshnessNoHistory}
	case sessionsBehind != nil && *sessionsBehind == 0:
		row.Freshness = Freshness{State: FreshnessCurrent, SessionsBehind: sessionsBehind}
	default:
		row.Freshness = Freshness{State: FreshnessStale, SessionsBehind: sessionsBehind}
	}
	return row, sortValue, nil
}

func decimalPointer(value *string) *Decimal {
	if value == nil {
		return nil
	}
	decimal := Decimal(*value)
	return &decimal
}
