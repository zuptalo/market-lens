package instruments

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrIdentityConflict = errors.New("instrument identity conflicts with existing data")
	ErrInvalidSync      = errors.New("instrument synchronization request is invalid")
	ErrInvalidQuery     = errors.New("instrument query is invalid")
	ErrNotFound         = errors.New("instrument not found")
)

type SyncListing struct {
	MIC                  string
	ExchangeName         string
	ExchangeCountry      string
	ExchangeCurrency     string
	ExchangeTimezone     string
	ISIN                 string
	Ticker               string
	Name                 string
	Currency             string
	Country              string
	Type                 InstrumentType
	Sector               string
	Industry             string
	PurchasabilityStatus PurchasabilityStatus
	ProviderSymbol       string
	CurationSource       string
	CurationNote         string
}

type SyncRequest struct {
	Provider      string
	UniverseCode  string
	UniverseName  string
	Description   string
	SelectionDate time.Time
	AppVersion    string
	Listings      []SyncListing
}

type SyncResult struct {
	RunID         UUID
	Status        string
	Counts        struct{ Processed, Accepted, Rejected, Flagged int64 }
	InstrumentIDs map[string]UUID
}

type Service struct {
	repository *Repository
}

func NewService(repository *Repository) *Service {
	return &Service{repository: repository}
}

type MarketDataReader interface {
	InstrumentSummary(context.Context, UUID) (MarketDataSummary, error)
	InstrumentPrices(context.Context, UUID, PriceFilter) (PricePage, error)
}

type QueryService struct {
	repository *Repository
	marketData MarketDataReader
}

func NewQueryService(repository *Repository, marketData MarketDataReader) *QueryService {
	return &QueryService{repository: repository, marketData: marketData}
}

func (s *QueryService) Search(ctx context.Context, filter SearchFilter) (SearchPage, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	filter.MIC = strings.ToUpper(strings.TrimSpace(filter.MIC))
	filter.Country = strings.ToUpper(strings.TrimSpace(filter.Country))
	filter.Currency = strings.ToUpper(strings.TrimSpace(filter.Currency))
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if len(filter.Query) > 120 || !validCode(filter.MIC, 4) || !validCode(filter.Country, 2) ||
		!validCode(filter.Currency, 3) || filter.Limit < 1 || filter.Limit > 200 || len(filter.Cursor) > 512 {
		return SearchPage{}, ErrInvalidQuery
	}
	page, err := s.repository.SearchPage(ctx, filter)
	if errors.Is(err, errInvalidSearchCursor) {
		return SearchPage{}, fmt.Errorf("%w: %v", ErrInvalidQuery, err)
	}
	if err != nil {
		return SearchPage{}, err
	}
	return page, nil
}

func (s *QueryService) Inspect(ctx context.Context, id UUID) (Inspection, error) {
	identity, err := s.repository.GetResult(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Inspection{}, ErrNotFound
	}
	if err != nil {
		return Inspection{}, err
	}
	summary, err := s.marketData.InstrumentSummary(ctx, id)
	if err != nil {
		return Inspection{}, err
	}
	return Inspection{Identity: identity, MarketDataSummary: summary}, nil
}

func (s *QueryService) Prices(ctx context.Context, id UUID, filter PriceFilter) (PricePage, error) {
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if filter.Limit < 1 || filter.Limit > 200 || (filter.From != "" && filter.To != "" && filter.From > filter.To) {
		return PricePage{}, ErrInvalidQuery
	}
	if filter.Cursor != "" {
		if _, err := ParseSessionDate(filter.Cursor); err != nil {
			return PricePage{}, ErrInvalidQuery
		}
	}
	if _, err := s.repository.GetResult(ctx, id); errors.Is(err, pgx.ErrNoRows) {
		return PricePage{}, ErrNotFound
	} else if err != nil {
		return PricePage{}, err
	}
	return s.marketData.InstrumentPrices(ctx, id, filter)
}

func validCode(value string, length int) bool {
	if value == "" {
		return true
	}
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func (s *Service) SyncUniverse(ctx context.Context, request SyncRequest) (SyncResult, error) {
	if err := validateSyncRequest(request); err != nil {
		return SyncResult{}, err
	}
	runID, err := NewUUID()
	if err != nil {
		return SyncResult{}, err
	}
	startedAt := time.Now().UTC()
	if _, err := s.repository.pool.Exec(ctx, `INSERT INTO import_runs
		(id, kind, provider, status, started_at, app_version)
		VALUES ($1, 'universe_sync', $2, 'running', $3, $4)`,
		runID.String(), request.Provider, startedAt, request.AppVersion); err != nil {
		return SyncResult{}, fmt.Errorf("start universe synchronization: %w", err)
	}

	result, syncErr := s.synchronize(ctx, request)
	if syncErr != nil {
		if _, err := s.repository.pool.Exec(ctx, `UPDATE import_runs SET status = 'failed', finished_at = $2,
			processed_count = $3, accepted_count = 0, rejected_count = $3,
			error_code = 'identity_conflict', error_summary = 'Instrument identity conflicts with retained market data.'
			WHERE id = $1`, runID.String(), time.Now().UTC(), int64(len(request.Listings))); err != nil {
			return SyncResult{}, fmt.Errorf("%w (also failed to retain run status)", syncErr)
		}
		return SyncResult{}, syncErr
	}
	if _, err := s.repository.pool.Exec(ctx, `UPDATE import_runs SET status = 'succeeded', finished_at = $2,
		processed_count = $3, accepted_count = $3 WHERE id = $1`,
		runID.String(), time.Now().UTC(), int64(len(request.Listings))); err != nil {
		return SyncResult{}, fmt.Errorf("finish universe synchronization: %w", err)
	}
	result.RunID = runID
	result.Status = "succeeded"
	result.Counts.Processed = int64(len(request.Listings))
	result.Counts.Accepted = int64(len(request.Listings))
	return result, nil
}

func validateSyncRequest(request SyncRequest) error {
	if strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.UniverseCode) == "" ||
		strings.TrimSpace(request.UniverseName) == "" || request.SelectionDate.IsZero() || strings.TrimSpace(request.AppVersion) == "" {
		return ErrInvalidSync
	}
	seen := make(map[string]struct{}, len(request.Listings))
	for _, listing := range request.Listings {
		if listing.MIC == "" || listing.ISIN == "" || listing.Ticker == "" || listing.Name == "" ||
			listing.ProviderSymbol == "" || listing.CurationSource == "" || listing.Type != InstrumentTypeCommonStock {
			return ErrInvalidSync
		}
		if _, duplicate := seen[listing.ProviderSymbol]; duplicate {
			return ErrIdentityConflict
		}
		seen[listing.ProviderSymbol] = struct{}{}
	}
	return nil
}

func (s *Service) synchronize(ctx context.Context, request SyncRequest) (SyncResult, error) {
	tx, err := s.repository.begin(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	universeID, err := upsertUniverse(ctx, tx, request)
	if err != nil {
		return SyncResult{}, err
	}
	result := SyncResult{InstrumentIDs: make(map[string]UUID, len(request.Listings))}
	present := make([]string, 0, len(request.Listings))
	for _, listing := range request.Listings {
		exchangeID, err := upsertExchange(ctx, tx, listing)
		if err != nil {
			return SyncResult{}, err
		}
		instrumentID, err := upsertListing(ctx, tx, request.Provider, exchangeID, listing)
		if err != nil {
			return SyncResult{}, err
		}
		if err := upsertMembership(ctx, tx, universeID, instrumentID, request.SelectionDate, listing); err != nil {
			return SyncResult{}, err
		}
		result.InstrumentIDs[listing.ProviderSymbol] = instrumentID
		present = append(present, instrumentID.String())
	}
	if err := inactivateMissing(ctx, tx, universeID, request.Provider, request.SelectionDate, present); err != nil {
		return SyncResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SyncResult{}, err
	}
	return result, nil
}

func upsertUniverse(ctx context.Context, tx pgx.Tx, request SyncRequest) (UUID, error) {
	var value string
	err := tx.QueryRow(ctx, `SELECT id::text FROM research_universes WHERE code = $1`, request.UniverseCode).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		id, idErr := NewUUID()
		if idErr != nil {
			return "", idErr
		}
		_, err = tx.Exec(ctx, `INSERT INTO research_universes (id, code, name, description, active)
			VALUES ($1, $2, $3, $4, true)`, id.String(), request.UniverseCode, request.UniverseName, request.Description)
		return id, err
	}
	if err != nil {
		return "", err
	}
	id, err := ParseUUID(value)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `UPDATE research_universes SET name = $2, description = $3, active = true, updated_at = now() WHERE id = $1`,
		id.String(), request.UniverseName, request.Description)
	return id, err
}

func upsertExchange(ctx context.Context, tx pgx.Tx, listing SyncListing) (UUID, error) {
	var value string
	err := tx.QueryRow(ctx, `SELECT id::text FROM exchanges WHERE mic = $1`, listing.MIC).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		id, idErr := NewUUID()
		if idErr != nil {
			return "", idErr
		}
		_, err = tx.Exec(ctx, `INSERT INTO exchanges (id, mic, name, country, currency, timezone, active)
			VALUES ($1, $2, $3, $4, $5, $6, true)`, id.String(), listing.MIC, listing.ExchangeName,
			listing.ExchangeCountry, listing.ExchangeCurrency, listing.ExchangeTimezone)
		return id, err
	}
	if err != nil {
		return "", err
	}
	id, err := ParseUUID(value)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `UPDATE exchanges SET name = $2, country = $3, currency = $4, timezone = $5,
		active = true, updated_at = now() WHERE id = $1`, id.String(), listing.ExchangeName, listing.ExchangeCountry,
		listing.ExchangeCurrency, listing.ExchangeTimezone)
	return id, err
}

func upsertListing(ctx context.Context, tx pgx.Tx, provider string, exchangeID UUID, listing SyncListing) (UUID, error) {
	var mappedID, mappedExchange, mappedISIN string
	err := tx.QueryRow(ctx, `SELECT i.id::text, i.exchange_id::text, i.isin FROM provider_instruments p
		JOIN instruments i ON i.id = p.instrument_id WHERE p.provider = $1 AND p.provider_symbol = $2`,
		provider, listing.ProviderSymbol).Scan(&mappedID, &mappedExchange, &mappedISIN)
	var instrumentID UUID
	if err == nil {
		if mappedExchange != exchangeID.String() || mappedISIN != listing.ISIN {
			return "", ErrIdentityConflict
		}
		instrumentID, err = ParseUUID(mappedID)
	} else if errors.Is(err, pgx.ErrNoRows) {
		var existingID, existingISIN, existingTicker string
		err = tx.QueryRow(ctx, `SELECT id::text, isin, ticker FROM instruments
			WHERE exchange_id = $1 AND (isin = $2 OR ticker = $3) LIMIT 1`,
			exchangeID.String(), listing.ISIN, listing.Ticker).Scan(&existingID, &existingISIN, &existingTicker)
		if err == nil {
			_ = existingTicker
			if existingISIN != listing.ISIN {
				return "", ErrIdentityConflict
			}
			instrumentID, err = ParseUUID(existingID)
		} else if errors.Is(err, pgx.ErrNoRows) {
			instrumentID, err = NewUUID()
			if err == nil {
				_, err = tx.Exec(ctx, `INSERT INTO instruments
					(id, exchange_id, isin, ticker, name, currency, country, instrument_type, sector, industry, active, purchasability_status)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),true,$11)`, instrumentID.String(),
					exchangeID.String(), listing.ISIN, listing.Ticker, listing.Name, listing.Currency, listing.Country,
					listing.Type, listing.Sector, listing.Industry, listing.PurchasabilityStatus)
			}
		}
	}
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE instruments SET ticker=$2, name=$3, currency=$4, country=$5,
		instrument_type=$6, sector=NULLIF($7,''), industry=NULLIF($8,''), active=true,
		purchasability_status=$9, updated_at=now() WHERE id=$1`, instrumentID.String(), listing.Ticker,
		listing.Name, listing.Currency, listing.Country, listing.Type, listing.Sector, listing.Industry,
		listing.PurchasabilityStatus); err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO provider_instruments (provider, provider_symbol, instrument_id, active)
		VALUES ($1,$2,$3,true) ON CONFLICT (provider, provider_symbol) DO UPDATE
		SET instrument_id=excluded.instrument_id, active=true, updated_at=now()`, provider, listing.ProviderSymbol, instrumentID.String())
	return instrumentID, err
}

func upsertMembership(ctx context.Context, tx pgx.Tx, universeID, instrumentID UUID, selectionDate time.Time, listing SyncListing) error {
	date := selectionDate.Format("2006-01-02")
	result, err := tx.Exec(ctx, `UPDATE universe_memberships SET included_to=NULL, curation_source=$3,
		curation_note=$4, updated_at=now() WHERE universe_id=$1 AND instrument_id=$2 AND included_to IS NULL`,
		universeID.String(), instrumentID.String(), listing.CurationSource, listing.CurationNote)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		_, err = tx.Exec(ctx, `INSERT INTO universe_memberships
			(universe_id,instrument_id,included_from,curation_source,curation_note)
			VALUES ($1,$2,$3,$4,$5) ON CONFLICT (universe_id,instrument_id,included_from) DO UPDATE
			SET included_to=NULL, curation_source=excluded.curation_source, curation_note=excluded.curation_note, updated_at=now()`,
			universeID.String(), instrumentID.String(), date, listing.CurationSource, listing.CurationNote)
	}
	return err
}

func inactivateMissing(ctx context.Context, tx pgx.Tx, universeID UUID, provider string, selectionDate time.Time, present []string) error {
	rows, err := tx.Query(ctx, `SELECT instrument_id::text FROM universe_memberships
		WHERE universe_id=$1 AND included_to IS NULL AND NOT (instrument_id::text = ANY($2))`, universeID.String(), present)
	if err != nil {
		return err
	}
	var missing []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		missing = append(missing, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(missing) == 0 {
		return nil
	}
	date := selectionDate.Format("2006-01-02")
	if _, err := tx.Exec(ctx, `UPDATE universe_memberships SET included_to=$2, updated_at=now()
		WHERE universe_id=$1 AND instrument_id::text = ANY($3) AND included_to IS NULL`, universeID.String(), date, missing); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE instruments SET active=false, updated_at=now() WHERE id::text = ANY($1)`, missing); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE provider_instruments SET active=false, updated_at=now()
		WHERE provider=$1 AND instrument_id::text = ANY($2)`, provider, missing)
	return err
}
