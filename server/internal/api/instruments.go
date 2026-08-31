package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"market-lens/server/internal/httpx"
	"market-lens/server/internal/instruments"
)

type InstrumentReader interface {
	Listing(context.Context, instruments.ListingFilter) (instruments.ListingPage, error)
	Inspect(context.Context, instruments.UUID) (instruments.Inspection, error)
	Prices(context.Context, instruments.UUID, instruments.PriceFilter) (instruments.PricePage, error)
}

type exchangeResponse struct {
	MIC      string `json:"mic"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
}

type freshnessResponse struct {
	State          string `json:"state"`
	SessionsBehind *int   `json:"sessions_behind"`
}

// listingRowResponse is the wire shape of one row of the universe.
//
// Every derived statistic is a pointer so that an absent one serializes as null. Using a
// value type here would turn "there were too few sessions to compute this" into "this
// instrument did not move", which is a different and false claim (FR-007). Money stays a
// decimal string for the same reason it does everywhere else: a float would quietly change
// the number.
type listingRowResponse struct {
	ID             string                           `json:"id"`
	ISIN           string                           `json:"isin"`
	Ticker         string                           `json:"ticker"`
	Name           string                           `json:"name"`
	Exchange       exchangeResponse                 `json:"exchange"`
	Currency       string                           `json:"currency"`
	Country        string                           `json:"country"`
	Sector         string                           `json:"sector"`
	Industry       string                           `json:"industry"`
	InstrumentType instruments.InstrumentType       `json:"instrument_type"`
	Status         string                           `json:"status"`
	Purchasability instruments.PurchasabilityStatus `json:"purchasability_status"`
	LatestSession  *string                          `json:"latest_session"`
	LatestClose    *string                          `json:"latest_close"`
	ChangeAbsolute *string                          `json:"change_absolute"`
	ChangePercent  *float64                         `json:"change_percent"`
	Return20       *float64                         `json:"return_20"`
	Return90       *float64                         `json:"return_90"`
	Volatility     *float64                         `json:"volatility"`
	StoredSessions int64                            `json:"stored_sessions"`
	Freshness      freshnessResponse                `json:"freshness"`
}

type dailyBarResponse struct {
	SessionDate   instruments.SessionDate `json:"session_date"`
	Open          instruments.Decimal     `json:"open"`
	High          instruments.Decimal     `json:"high"`
	Low           instruments.Decimal     `json:"low"`
	Close         instruments.Decimal     `json:"close"`
	AdjustedClose *string                 `json:"adjusted_close"`
	Volume        int64                   `json:"volume"`
	Currency      string                  `json:"currency"`
	Provider      string                  `json:"provider"`
	ObservedAt    time.Time               `json:"observed_at"`
}

// listInstrumentsHandler answers the universe list with price, derived statistics and
// freshness. Its query vocabulary is the contract's — mic, sector, status — rather than the
// words the identity-only search happened to grow first.
func listInstrumentsHandler(reader InstrumentReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		filter := instruments.ListingFilter{
			Query:    query.Get("q"),
			MIC:      strings.ToUpper(query.Get("mic")),
			Country:  strings.ToUpper(query.Get("country")),
			Currency: strings.ToUpper(query.Get("currency")),
			Sector:   query.Get("sector"),
			Status:   query.Get("status"),
			Sort:     instruments.ListingSort(query.Get("sort")),
			Cursor:   query.Get("cursor"),
		}
		if len(filter.Query) > 120 || len(filter.Cursor) > 512 || !validInstrumentCode(filter.MIC, 4) ||
			!validInstrumentCode(filter.Country, 2) || !validInstrumentCode(filter.Currency, 3) {
			httpx.Error(w, http.StatusBadRequest, "invalid instrument filter")
			return
		}
		switch filter.Status {
		case "", "active", "inactive":
		default:
			httpx.Error(w, http.StatusBadRequest, "invalid instrument status")
			return
		}
		if filter.Sort == "" {
			filter.Sort = instruments.SortName
		}
		if !instruments.SupportedListingSort(filter.Sort) {
			httpx.Error(w, http.StatusBadRequest, "invalid sort")
			return
		}
		switch order := query.Get("order"); order {
		case "", "asc":
		case "desc":
			filter.Descending = true
		default:
			httpx.Error(w, http.StatusBadRequest, "invalid sort order")
			return
		}
		limit, ok := instrumentLimit(r, 50)
		if !ok {
			httpx.Error(w, http.StatusBadRequest, "invalid limit")
			return
		}
		filter.Limit = limit
		page, err := reader.Listing(r.Context(), filter)
		if err != nil {
			writeInstrumentError(w, err)
			return
		}
		items := make([]listingRowResponse, 0, len(page.Items))
		for _, item := range page.Items {
			items = append(items, listingRowDTO(item))
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nullableString(page.NextCursor)})
	}
}

func getInstrumentHandler(reader InstrumentReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := instruments.ParseUUID(r.PathValue("id"))
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid instrument ID")
			return
		}
		inspection, err := reader.Inspect(r.Context(), id)
		if err != nil {
			writeInstrumentError(w, err)
			return
		}
		body := map[string]any{
			"id": inspection.Identity.ID.String(), "isin": inspection.Identity.ISIN,
			"ticker": inspection.Identity.Ticker, "name": inspection.Identity.Name,
			"exchange": exchangeDTO(inspection.Identity.Exchange), "currency": inspection.Identity.Currency,
			"country": inspection.Identity.Country, "instrument_type": inspection.Identity.Type,
			"active": inspection.Identity.Active, "purchasability_status": inspection.Identity.PurchasabilityStatus,
			"latest_bar":      dailyBarDTO(inspection.LatestBar),
			"history":         map[string]any{"first_session": nullableDate(inspection.Coverage.FirstSession), "last_session": nullableDate(inspection.Coverage.LastSession), "bar_count": inspection.Coverage.BarCount},
			"quality_summary": map[string]int64{"open_warnings": inspection.Quality.OpenWarnings, "open_errors": inspection.Quality.OpenErrors},
		}
		httpx.JSON(w, http.StatusOK, body)
	}
}

func listInstrumentPricesHandler(reader InstrumentReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := instruments.ParseUUID(r.PathValue("id"))
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid instrument ID")
			return
		}
		filter := instruments.PriceFilter{Cursor: r.URL.Query().Get("cursor")}
		if raw := r.URL.Query().Get("from"); raw != "" {
			if filter.From, err = instruments.ParseSessionDate(raw); err != nil {
				httpx.Error(w, http.StatusBadRequest, "invalid from date")
				return
			}
		}
		if raw := r.URL.Query().Get("to"); raw != "" {
			if filter.To, err = instruments.ParseSessionDate(raw); err != nil {
				httpx.Error(w, http.StatusBadRequest, "invalid to date")
				return
			}
		}
		if filter.Cursor != "" {
			if _, err := instruments.ParseSessionDate(filter.Cursor); err != nil {
				httpx.Error(w, http.StatusBadRequest, "invalid price cursor")
				return
			}
		}
		limit, ok := instrumentLimit(r, 50)
		if !ok || (filter.From != "" && filter.To != "" && filter.From > filter.To) {
			httpx.Error(w, http.StatusBadRequest, "invalid price range")
			return
		}
		filter.Limit = limit
		page, err := reader.Prices(r.Context(), id, filter)
		if err != nil {
			writeInstrumentError(w, err)
			return
		}
		items := make([]dailyBarResponse, 0, len(page.Items))
		for index := range page.Items {
			items = append(items, *dailyBarDTO(&page.Items[index]))
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nullableString(page.NextCursor)})
	}
}

func listingRowDTO(item instruments.ListingRow) listingRowResponse {
	status := "inactive"
	if item.Active {
		status = "active"
	}
	return listingRowResponse{
		ID: item.ID.String(), ISIN: item.ISIN, Ticker: item.Ticker, Name: item.Name,
		Exchange: exchangeDTO(item.Exchange), Currency: item.Currency, Country: item.Country,
		Sector: item.Sector, Industry: item.Industry, InstrumentType: item.Type,
		Status: status, Purchasability: item.PurchasabilityStatus,
		LatestSession:  optionalSession(item.LatestSession),
		LatestClose:    optionalDecimal(item.LatestClose),
		ChangeAbsolute: optionalDecimal(item.ChangeAbsolute),
		ChangePercent:  item.ChangePercent,
		Return20:       item.Return20,
		Return90:       item.Return90,
		Volatility:     item.Volatility,
		StoredSessions: item.StoredSessions,
		Freshness: freshnessResponse{
			State: string(item.Freshness.State), SessionsBehind: item.Freshness.SessionsBehind,
		},
	}
}

func optionalDecimal(value *instruments.Decimal) *string {
	if value == nil {
		return nil
	}
	text := value.String()
	return &text
}

func optionalSession(value instruments.SessionDate) *string {
	if value == "" {
		return nil
	}
	text := value.String()
	return &text
}

func exchangeDTO(exchange instruments.Exchange) exchangeResponse {
	return exchangeResponse{MIC: exchange.MIC, Name: exchange.Name, Timezone: exchange.Timezone}
}

func dailyBarDTO(bar *instruments.DailyBar) *dailyBarResponse {
	if bar == nil {
		return nil
	}
	return &dailyBarResponse{SessionDate: bar.SessionDate, Open: bar.Open, High: bar.High, Low: bar.Low,
		Close: bar.Close, AdjustedClose: bar.AdjustedClose, Volume: bar.Volume, Currency: bar.Currency,
		Provider: bar.Provider, ObservedAt: bar.ObservedAt}
}

func instrumentLimit(r *http.Request, defaultValue int) (int, bool) {
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		return value, err == nil && value >= 1 && value <= 200
	}
	return defaultValue, true
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableDate(value instruments.SessionDate) any { return nullableString(value.String()) }

func validInstrumentCode(value string, length int) bool {
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

func writeInstrumentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, instruments.ErrInvalidQuery):
		httpx.Error(w, http.StatusBadRequest, "invalid instrument query")
	case errors.Is(err, instruments.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not found")
	default:
		httpx.Error(w, http.StatusInternalServerError, "instrument request failed")
	}
}
