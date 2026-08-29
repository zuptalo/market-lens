package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"market-lens/server/internal/api"
	"market-lens/server/internal/config"
	"market-lens/server/internal/db"
	clientevents "market-lens/server/internal/events"
	"market-lens/server/internal/instruments"
	"market-lens/server/internal/marketdata"
	"market-lens/server/internal/marketdata/eodhd"
	"market-lens/server/internal/scheduler"
)

var version = "dev"

type marketDataCommand struct {
	Kind     marketdata.ImportKind
	Universe string
	From     marketdata.SessionDate
	To       marketdata.SessionDate
	RunID    instruments.UUID
}

type marketDataImporter interface {
	Import(context.Context, marketdata.ImportRequest) (marketdata.ImportRun, error)
}

type marketDataRetrier interface {
	Retry(context.Context, marketdata.RetryRequest) (marketdata.ImportRun, error)
}

func executeMarketDataRetry(ctx context.Context, command marketDataCommand, retrier marketDataRetrier,
	output io.Writer, appVersion string, maxRetries, workers int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	run, err := retrier.Retry(ctx, marketdata.RetryRequest{
		ParentRunID: command.RunID, AppVersion: appVersion, MaxRetries: maxRetries, Workers: workers,
	})
	if err != nil {
		return err
	}
	return writeImportTotals(output, run)
}

func parseMarketDataCommand(args []string, now time.Time) (marketDataCommand, error) {
	if len(args) < 2 || args[0] != "marketdata" {
		return marketDataCommand{}, errors.New("expected marketdata backfill, marketdata update, or marketdata retry")
	}
	if args[1] == "retry" {
		flags := flag.NewFlagSet("marketdata retry", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		var rawRunID string
		flags.StringVar(&rawRunID, "run", "", "failed or partial parent run ID")
		if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 {
			return marketDataCommand{}, errors.New("retry run ID is invalid")
		}
		runID, err := instruments.ParseUUID(rawRunID)
		if err != nil {
			return marketDataCommand{}, errors.New("retry run ID is invalid")
		}
		return marketDataCommand{Kind: marketdata.ImportRetry, RunID: runID}, nil
	}
	command := marketDataCommand{Universe: "nordic-liquid-v1"}
	flags := flag.NewFlagSet("marketdata "+args[1], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&command.Universe, "universe", command.Universe, "research universe code")
	to, err := marketdata.ParseSessionDate(now.UTC().Format("2006-01-02"))
	if err != nil {
		return marketDataCommand{}, err
	}
	command.To = to
	switch args[1] {
	case "backfill":
		years := flags.Int("years", 10, "inclusive number of years to request")
		if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 || *years < 1 || *years > 30 {
			return marketDataCommand{}, errors.New("backfill years must be between 1 and 30")
		}
		command.Kind = marketdata.ImportBackfill
		command.From, err = marketdata.ParseSessionDate(now.UTC().AddDate(-*years, 0, 0).Format("2006-01-02"))
	case "update":
		days := flags.Int("days", 7, "inclusive number of calendar days to request")
		if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 || *days < 1 || *days > 31 {
			return marketDataCommand{}, errors.New("update days must be between 1 and 31")
		}
		command.Kind = marketdata.ImportDailyUpdate
		command.From, err = marketdata.ParseSessionDate(now.UTC().AddDate(0, 0, -(*days - 1)).Format("2006-01-02"))
	default:
		return marketDataCommand{}, errors.New("expected marketdata backfill, marketdata update, or marketdata retry")
	}
	if err != nil || command.Universe == "" {
		return marketDataCommand{}, errors.New("market-data command scope is invalid")
	}
	return command, nil
}

func executeMarketDataCommand(ctx context.Context, command marketDataCommand, targets []marketdata.ImportTarget,
	importer marketDataImporter, output io.Writer, provider, appVersion string, maxRetries, workers int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for index := range targets {
		targets[index].From = command.From
		targets[index].To = command.To
	}
	run, err := importer.Import(ctx, marketdata.ImportRequest{
		Kind: command.Kind, Provider: provider, AppVersion: appVersion, Targets: targets,
		MaxRetries: maxRetries, Workers: workers,
	})
	if err != nil {
		return err
	}
	return writeImportTotals(output, run)
}

func writeImportTotals(output io.Writer, run marketdata.ImportRun) error {
	_, err := fmt.Fprintf(output, "run_id=%s status=%s processed=%d accepted=%d rejected=%d flagged=%d\n",
		run.ID, run.Status, run.Counts.Processed, run.Counts.Accepted, run.Counts.Rejected, run.Counts.Flagged)
	return err
}

func main() {
	if err := run(); err != nil {
		slog.Error("market lens stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	configureLogging(cfg.IsProduction())
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	if len(os.Args) > 1 {
		if os.Args[1] != "marketdata" {
			return errors.New("unknown command")
		}
		command, err := parseMarketDataCommand(os.Args[1:], time.Now())
		if err != nil {
			return err
		}
		if cfg.MarketData.Provider != "eodhd" {
			return errors.New("configured market-data provider is not supported")
		}
		provider, err := eodhd.New(eodhd.Config{
			APIToken:   cfg.MarketData.APIToken,
			HTTPClient: &http.Client{Timeout: cfg.MarketData.RequestTimeout},
		})
		if err != nil {
			return err
		}
		repository := marketdata.NewRepository(pool)
		service := marketdata.NewImportService(repository, provider)
		if command.Kind == marketdata.ImportRetry {
			return executeMarketDataRetry(ctx, command, service, os.Stdout, version,
				cfg.MarketData.MaxRetries, cfg.MarketData.Workers)
		}
		targets, err := repository.TargetsForUniverse(ctx, cfg.MarketData.Provider, command.Universe)
		if err != nil {
			return err
		}
		return executeMarketDataCommand(ctx, command, targets, service, os.Stdout,
			cfg.MarketData.Provider, version, cfg.MarketData.MaxRetries, cfg.MarketData.Workers)
	}

	var scheduleErr <-chan error
	if cfg.MarketData.ScheduleEnabled {
		if cfg.MarketData.Provider != "eodhd" {
			return errors.New("configured market-data provider is not supported")
		}
		provider, err := eodhd.New(eodhd.Config{
			APIToken:   cfg.MarketData.APIToken,
			HTTPClient: &http.Client{Timeout: cfg.MarketData.RequestTimeout},
		})
		if err != nil {
			return err
		}
		repository := marketdata.NewRepository(pool)
		service := marketdata.NewImportService(repository, provider)
		job, err := scheduler.NewMarketData(scheduler.MarketDataConfig{
			Enabled: true, Hour: cfg.MarketData.DailyHour, Minute: cfg.MarketData.DailyMinute,
			Location: cfg.MarketData.DailyLocation, Provider: cfg.MarketData.Provider,
			Universe: "nordic-liquid-v1", AppVersion: version,
			MaxRetries: cfg.MarketData.MaxRetries, Workers: cfg.MarketData.Workers,
		}, repository, service)
		if err != nil {
			return err
		}
		jobErrors := make(chan error, 1)
		scheduleErr = jobErrors
		go func() { jobErrors <- job.Run(ctx) }()
	}

	handler := api.NewRouter(api.Dependencies{
		Database: pool, AllowedOrigins: cfg.AllowedOrigins, StaticDir: cfg.StaticDir, Version: version,
		MarketData: marketdata.NewRepository(pool), Events: clientevents.NewRepository(pool), EventScope: "shared",
	})
	server := &http.Server{
		Addr: ":" + cfg.Port, Handler: handler,
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("market lens starting", "address", server.Addr, "environment", cfg.Environment, "version", version)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		slog.Info("shutting down")
		return server.Shutdown(shutdownCtx)
	case err := <-scheduleErr:
		if err == nil {
			return nil
		}
		return err
	}
}

func configureLogging(production bool) {
	options := &slog.HandlerOptions{Level: slog.LevelInfo}
	if production {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, options)))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, options)))
	}
}
