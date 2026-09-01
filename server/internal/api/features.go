package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"market-lens/server/internal/features"
	"market-lens/server/internal/httpx"
	"market-lens/server/internal/instruments"
)

// FeatureReader reads the feature store. There is deliberately no writer: no HTTP route
// triggers a computation (spec 013, security evidence).
type FeatureReader interface {
	Read(context.Context, features.ReadRequest) (features.FeatureSet, error)
	ListDefinitions(ctx context.Context, name string, includeSuperseded bool) ([]features.Definition, error)
}

type compositeReferenceResponse struct {
	Composite        string `json:"composite"`
	Version          int    `json:"version"`
	ContributorCount int    `json:"contributorCount"`
}

// featureValueResponse mirrors FeatureValue in contracts/openapi.yaml. Value is a decimal
// string, never a JSON number: the stored column is numeric(24,12).
type featureValueResponse struct {
	Name              string                      `json:"name"`
	DefinitionVersion int                         `json:"definitionVersion"`
	WindowSessions    *int                        `json:"windowSessions"`
	Value             *string                     `json:"value"`
	Label             *string                     `json:"label"`
	AbsenceReason     *string                     `json:"absenceReason"`
	Currency          *string                     `json:"currency,omitempty"`
	ComparedTo        *compositeReferenceResponse `json:"comparedTo,omitempty"`
	ComputedAt        time.Time                   `json:"computedAt"`
}

type instrumentFeaturesResponse struct {
	InstrumentID string                 `json:"instrumentId"`
	SessionDate  string                 `json:"sessionDate"`
	Features     []featureValueResponse `json:"features"`
	NotComputed  []string               `json:"notComputed"`
}

type featureDefinitionResponse struct {
	Name                   string         `json:"name"`
	Version                int            `json:"version"`
	WindowSessions         *int           `json:"windowSessions"`
	PriceBasis             string         `json:"priceBasis"`
	Parameters             map[string]any `json:"parameters"`
	UndefinedConditions    string         `json:"undefinedConditions"`
	SessionLengthSensitive bool           `json:"sessionLengthSensitive"`
	PublishedAt            time.Time      `json:"publishedAt"`
	SupersededAt           *time.Time     `json:"supersededAt"`
}

func getInstrumentFeaturesHandler(reader FeatureReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := instruments.ParseUUID(r.PathValue("id"))
		if err != nil {
			writeFeatureError(w, http.StatusBadRequest, "invalid_instrument_id", "The instrument identifier is invalid.", nil)
			return
		}
		request := features.ReadRequest{InstrumentID: id, Features: r.URL.Query()["feature"]}
		if raw := r.URL.Query().Get("asOf"); raw != "" {
			if request.AsOf, err = instruments.ParseSessionDate(raw); err != nil {
				writeFeatureError(w, http.StatusBadRequest, "invalid_as_of", "asOf must be a valid YYYY-MM-DD date.", nil)
				return
			}
		}
		set, err := reader.Read(r.Context(), request)
		if err != nil {
			var unknown *features.UnknownFeatureError
			switch {
			case errors.As(err, &unknown):
				writeFeatureError(w, http.StatusBadRequest, "unknown_feature",
					"There is no feature named "+unknown.Name+".", unknown.Known)
			case errors.Is(err, features.ErrClosedDate):
				writeFeatureError(w, http.StatusBadRequest, "closed_date",
					"The exchange was not open on the requested date; there is no session to read.", nil)
			case errors.Is(err, features.ErrNoHistory):
				// The same answer for an instrument that does not exist and one with no history
				// before the date, so the response never reveals which identifiers are real.
				writeFeatureError(w, http.StatusNotFound, "no_history",
					"There is no history for this instrument on or before the requested session.", nil)
			default:
				writeFeatureError(w, http.StatusInternalServerError, "features_unavailable", "The feature request failed.", nil)
			}
			return
		}
		httpx.JSON(w, http.StatusOK, instrumentFeaturesDTO(set))
	}
}

func listFeatureDefinitionsHandler(reader FeatureReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		includeSuperseded := true
		if raw := r.URL.Query().Get("includeSuperseded"); raw != "" {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				writeFeatureError(w, http.StatusBadRequest, "invalid_include_superseded", "includeSuperseded must be true or false.", nil)
				return
			}
			includeSuperseded = value
		}
		definitions, err := reader.ListDefinitions(r.Context(), r.URL.Query().Get("name"), includeSuperseded)
		if err != nil {
			writeFeatureError(w, http.StatusInternalServerError, "features_unavailable", "The definition request failed.", nil)
			return
		}
		items := make([]featureDefinitionResponse, 0, len(definitions))
		for _, definition := range definitions {
			parameters := definition.Parameters
			if parameters == nil {
				parameters = map[string]any{}
			}
			items = append(items, featureDefinitionResponse{
				Name: definition.Name, Version: definition.Version, WindowSessions: definition.WindowSessions,
				PriceBasis: string(definition.PriceBasis), Parameters: parameters,
				UndefinedConditions: definition.UndefinedConditions, SessionLengthSensitive: definition.SessionLengthSensitive,
				PublishedAt: definition.PublishedAt, SupersededAt: definition.SupersededAt,
			})
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func instrumentFeaturesDTO(set features.FeatureSet) instrumentFeaturesResponse {
	response := instrumentFeaturesResponse{
		InstrumentID: set.InstrumentID.String(), SessionDate: set.SessionDate.String(),
		Features: make([]featureValueResponse, 0, len(set.Features)), NotComputed: set.NotComputed,
	}
	if response.NotComputed == nil {
		response.NotComputed = []string{}
	}
	for _, value := range set.Features {
		item := featureValueResponse{
			Name: value.Name, DefinitionVersion: value.DefinitionVersion, WindowSessions: value.WindowSessions,
			Value: value.Value, Label: value.Label, Currency: value.Currency, ComputedAt: value.ComputedAt,
		}
		if value.AbsenceReason != nil {
			reason := string(*value.AbsenceReason)
			item.AbsenceReason = &reason
		}
		if value.ComparedTo != nil {
			item.ComparedTo = &compositeReferenceResponse{
				Composite: value.ComparedTo.Composite, Version: value.ComparedTo.Version,
				ContributorCount: value.ComparedTo.ContributorCount,
			}
		}
		response.Features = append(response.Features, item)
	}
	return response
}

// writeFeatureError writes the contract's Error envelope; knownFeatures appears only on an
// unknown-feature refusal.
func writeFeatureError(w http.ResponseWriter, status int, code, message string, knownFeatures []string) {
	body := map[string]any{"code": code, "message": message}
	if knownFeatures != nil {
		body["knownFeatures"] = knownFeatures
	}
	httpx.JSON(w, status, map[string]any{"error": body})
}
