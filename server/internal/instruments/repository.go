package instruments

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

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
