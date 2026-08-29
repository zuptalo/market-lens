package instruments

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

var errInvalidSearchCursor = errors.New("invalid instrument cursor")

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Get(ctx context.Context, id UUID) (Instrument, error) {
	return scanInstrument(r.pool.QueryRow(ctx, `SELECT id::text, exchange_id::text, isin, ticker, name,
		currency, country, instrument_type, coalesce(sector, ''), coalesce(industry, ''), active,
		purchasability_status, created_at, updated_at FROM instruments WHERE id = $1`, id.String()))
}

func (r *Repository) Search(ctx context.Context, query string, limit int) ([]Instrument, error) {
	if limit < 1 || limit > 200 {
		return nil, errors.New("instrument search limit must be between 1 and 200")
	}
	pattern := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	rows, err := r.pool.Query(ctx, `SELECT id::text, exchange_id::text, isin, ticker, name,
		currency, country, instrument_type, coalesce(sector, ''), coalesce(industry, ''), active,
		purchasability_status, created_at, updated_at FROM instruments
		WHERE $1 = '%%' OR lower(ticker) LIKE $1 OR lower(name) LIKE $1 OR lower(isin) LIKE $1
		ORDER BY ticker, exchange_id, id LIMIT $2`, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search instruments: %w", err)
	}
	defer rows.Close()
	result := make([]Instrument, 0)
	for rows.Next() {
		instrument, err := scanInstrument(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, instrument)
	}
	return result, rows.Err()
}

type searchCursor struct {
	Ticker string `json:"ticker"`
	MIC    string `json:"mic"`
	ID     string `json:"id"`
}

func (r *Repository) SearchPage(ctx context.Context, filter SearchFilter) (SearchPage, error) {
	if filter.Limit < 1 || filter.Limit > 200 {
		return SearchPage{}, errors.New("instrument search limit must be between 1 and 200")
	}
	cursor, err := decodeSearchCursor(filter.Cursor)
	if err != nil {
		return SearchPage{}, err
	}
	pattern := "%" + strings.ToLower(strings.TrimSpace(filter.Query)) + "%"
	rows, err := r.pool.Query(ctx, `SELECT i.id::text,i.exchange_id::text,i.isin,i.ticker,i.name,
		i.currency,i.country,i.instrument_type,coalesce(i.sector,''),coalesce(i.industry,''),i.active,
		i.purchasability_status,i.created_at,i.updated_at,e.id::text,e.mic,e.name,e.country,e.currency,
		e.timezone,e.active,e.created_at,e.updated_at
		FROM instruments i JOIN exchanges e ON e.id=i.exchange_id
		WHERE ($1='%%' OR lower(i.ticker) LIKE $1 OR lower(i.name) LIKE $1 OR lower(i.isin) LIKE $1)
		AND ($2='' OR e.mic=$2) AND ($3='' OR i.country=$3) AND ($4='' OR i.currency=$4)
		AND ($5::boolean IS NULL OR i.active=$5)
		AND ($6='' OR (lower(i.ticker),e.mic,i.id) > ($6,$7,$8::uuid))
		ORDER BY lower(i.ticker),e.mic,i.id LIMIT $9`, pattern, filter.MIC, filter.Country, filter.Currency,
		filter.Active, cursor.Ticker, cursor.MIC, nullableCursorID(cursor.ID), filter.Limit+1)
	if err != nil {
		return SearchPage{}, fmt.Errorf("search instrument page: %w", err)
	}
	defer rows.Close()
	items := make([]SearchResult, 0, filter.Limit+1)
	for rows.Next() {
		item, scanErr := scanSearchResult(rows)
		if scanErr != nil {
			return SearchPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return SearchPage{}, err
	}
	page := SearchPage{Items: items}
	if len(items) > filter.Limit {
		page.Items = items[:filter.Limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor, err = encodeSearchCursor(searchCursor{strings.ToLower(last.Ticker), last.Exchange.MIC, last.ID.String()})
		if err != nil {
			return SearchPage{}, err
		}
	}
	return page, nil
}

func (r *Repository) GetResult(ctx context.Context, id UUID) (SearchResult, error) {
	return scanSearchResult(r.pool.QueryRow(ctx, `SELECT i.id::text,i.exchange_id::text,i.isin,i.ticker,i.name,
		i.currency,i.country,i.instrument_type,coalesce(i.sector,''),coalesce(i.industry,''),i.active,
		i.purchasability_status,i.created_at,i.updated_at,e.id::text,e.mic,e.name,e.country,e.currency,
		e.timezone,e.active,e.created_at,e.updated_at FROM instruments i JOIN exchanges e ON e.id=i.exchange_id WHERE i.id=$1`, id.String()))
}

func scanSearchResult(row instrumentScanner) (SearchResult, error) {
	var result SearchResult
	var id, exchangeID, joinedExchangeID string
	err := row.Scan(&id, &exchangeID, &result.ISIN, &result.Ticker, &result.Name, &result.Currency,
		&result.Country, &result.Type, &result.Sector, &result.Industry, &result.Active,
		&result.PurchasabilityStatus, &result.CreatedAt, &result.UpdatedAt, &joinedExchangeID,
		&result.Exchange.MIC, &result.Exchange.Name, &result.Exchange.Country, &result.Exchange.Currency,
		&result.Exchange.Timezone, &result.Exchange.Active, &result.Exchange.CreatedAt, &result.Exchange.UpdatedAt)
	if err != nil {
		return SearchResult{}, err
	}
	if result.ID, err = ParseUUID(id); err != nil {
		return SearchResult{}, err
	}
	if result.ExchangeID, err = ParseUUID(exchangeID); err != nil {
		return SearchResult{}, err
	}
	if result.Exchange.ID, err = ParseUUID(joinedExchangeID); err != nil {
		return SearchResult{}, err
	}
	return result, nil
}

func decodeSearchCursor(value string) (searchCursor, error) {
	if value == "" {
		return searchCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return searchCursor{}, errInvalidSearchCursor
	}
	var cursor searchCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.Ticker == "" || cursor.MIC == "" {
		return searchCursor{}, errInvalidSearchCursor
	}
	if _, err := ParseUUID(cursor.ID); err != nil {
		return searchCursor{}, errInvalidSearchCursor
	}
	return cursor, nil
}

func encodeSearchCursor(cursor searchCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func nullableCursorID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

type instrumentScanner interface {
	Scan(...any) error
}

func scanInstrument(row instrumentScanner) (Instrument, error) {
	var instrument Instrument
	var id, exchangeID string
	if err := row.Scan(&id, &exchangeID, &instrument.ISIN, &instrument.Ticker, &instrument.Name,
		&instrument.Currency, &instrument.Country, &instrument.Type, &instrument.Sector, &instrument.Industry,
		&instrument.Active, &instrument.PurchasabilityStatus, &instrument.CreatedAt, &instrument.UpdatedAt); err != nil {
		return Instrument{}, err
	}
	var err error
	if instrument.ID, err = ParseUUID(id); err != nil {
		return Instrument{}, fmt.Errorf("scan instrument ID: %w", err)
	}
	if instrument.ExchangeID, err = ParseUUID(exchangeID); err != nil {
		return Instrument{}, fmt.Errorf("scan exchange ID: %w", err)
	}
	return instrument, nil
}

func (r *Repository) begin(ctx context.Context) (pgx.Tx, error) {
	return r.pool.Begin(ctx)
}
