// Package api wires the REST API, middleware, and production frontend serving.
package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"market-lens/server/internal/httpx"
)

type Database interface {
	Ping(context.Context) error
}

type Dependencies struct {
	Database        Database
	AllowedOrigins  []string
	StaticDir       string
	Version         string
	MarketData      MarketDataReader
	Events          EventReader
	EventScope      string
	EventHeartbeat  time.Duration
	EventBatchLimit int
}

func NewRouter(deps Dependencies) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{
			"status": "ok", "service": "market-lens", "version": deps.Version,
		})
	})
	mux.HandleFunc("GET /api/v1/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if deps.Database == nil || deps.Database.Ping(ctx) != nil {
			httpx.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	if deps.MarketData != nil {
		mux.HandleFunc("GET /api/v1/market-data/imports", listImportRunsHandler(deps.MarketData))
		mux.HandleFunc("GET /api/v1/market-data/imports/{id}", getImportRunHandler(deps.MarketData))
		mux.HandleFunc("GET /api/v1/market-data/quality-findings", listQualityFindingsHandler(deps.MarketData))
	}
	if deps.Events != nil {
		mux.HandleFunc("GET /api/v1/events", eventsHandler(deps.Events, deps.EventScope, deps.EventHeartbeat, deps.EventBatchLimit))
	}

	if deps.StaticDir != "" {
		mux.Handle("/", spaHandler(deps.StaticDir))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				httpx.Error(w, http.StatusNotFound, "not found")
				return
			}
			http.NotFound(w, r)
		})
	}
	return httpx.Chain(mux, httpx.Recover, httpx.Log, httpx.CORS(deps.AllowedOrigins))
}
