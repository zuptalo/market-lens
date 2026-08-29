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
	Search(context.Context, instruments.SearchFilter) (instruments.SearchPage, error)
	Inspect(context.Context, instruments.UUID) (instruments.Inspection, error)
	Prices(context.Context, instruments.UUID, instruments.PriceFilter) (instruments.PricePage, error)
}

type exchangeResponse struct {
	MIC      string `json:"mic"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
}

type instrumentResponse struct {
	ID                   string                           `json:"id"`
	ISIN                 string                           `json:"isin"`
	Ticker               string                           `json:"ticker"`
	Name                 string                           `json:"name"`
	Exchange             exchangeResponse                 `json:"exchange"`
	Currency             string                           `json:"currency"`
	Country              string                           `json:"country"`
	InstrumentType       instruments.InstrumentType       `json:"instrument_type"`
	Active               bool                             `json:"active"`
	PurchasabilityStatus instruments.PurchasabilityStatus `json:"purchasability_status"`
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

func listInstrumentsHandler(reader InstrumentReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter := instruments.SearchFilter{Query: r.URL.Query().Get("q"), MIC: strings.ToUpper(r.URL.Query().Get("exchange")), Country: strings.ToUpper(r.URL.Query().Get("country")), Currency: strings.ToUpper(r.URL.Query().Get("currency")), Cursor: r.URL.Query().Get("cursor")}
		if len(filter.Query) > 120 || len(filter.Cursor) > 512 || !validInstrumentCode(filter.MIC, 4) ||
			!validInstrumentCode(filter.Country, 2) || !validInstrumentCode(filter.Currency, 3) {
			httpx.Error(w, http.StatusBadRequest, "invalid instrument filter")
			return
		}
		if raw := r.URL.Query().Get("active"); raw != "" {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				httpx.Error(w, http.StatusBadRequest, "invalid active status")
				return
			}
			filter.Active = &value
		}
		limit, ok := instrumentLimit(r, 50)
		if !ok {
			httpx.Error(w, http.StatusBadRequest, "invalid limit")
			return
		}
		filter.Limit = limit
		page, err := reader.Search(r.Context(), filter)
		if err != nil {
			writeInstrumentError(w, err)
			return
		}
		items := make([]instrumentResponse, 0, len(page.Items))
		for _, item := range page.Items {
			items = append(items, instrumentDTO(item))
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

func instrumentDTO(item instruments.SearchResult) instrumentResponse {
	return instrumentResponse{ID: item.ID.String(), ISIN: item.ISIN, Ticker: item.Ticker, Name: item.Name,
		Exchange: exchangeDTO(item.Exchange), Currency: item.Currency, Country: item.Country,
		InstrumentType: item.Type, Active: item.Active, PurchasabilityStatus: item.PurchasabilityStatus}
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
