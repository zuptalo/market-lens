package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"market-lens/server/internal/instruments"
	"market-lens/server/internal/marketdata"
	"market-lens/server/internal/marketdata/yahoo"
	"market-lens/server/internal/notify"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The owner's fallback commands (feature 030). They are commands, like backfill and resolve,
// because every market-data operation already is one, and because the switch they set lives in the
// database: Keel deploys images, never configuration.

type fallbackOperations interface {
	Enabled(context.Context) (bool, error)
	SetEnabled(context.Context, bool) error
	State(context.Context) (marketdata.FallbackState, error)
	Observe(context.Context) (marketdata.FallbackState, bool, error)
	ReconcileTargets(context.Context, string) ([]marketdata.ImportTarget, error)
}

func validFallbackAction(action string) bool {
	switch action {
	case "status", "enable", "disable", "audit", "reconcile":
		return true
	}
	return false
}

// executeFallbackSwitch turns the fallback on or off, or reports it, and always ends by saying
// where it stands.
func executeFallbackSwitch(ctx context.Context, action string, ops fallbackOperations, output io.Writer) error {
	switch action {
	case "enable", "disable":
		if err := ops.SetEnabled(ctx, action == "enable"); err != nil {
			return err
		}
	case "status":
	default:
		return fmt.Errorf("unknown fallback action %q", action)
	}
	enabled, err := ops.Enabled(ctx)
	if err != nil {
		return err
	}
	state, err := ops.State(ctx)
	if err != nil {
		return err
	}
	since := "-"
	if state.Since != nil {
		since = state.Since.String()
	}
	_, err = fmt.Fprintf(output, "enabled=%t active=%t since=%s instruments=%d pending_reconciliation=%t\n",
		enabled, state.Active, since, state.Instruments, state.PendingReconciliation)
	return err
}

// observeClose asks the fallback what it reports for one symbol on one session.
type observeClose func(ctx context.Context, symbol string, session marketdata.SessionDate) (string, *marketdata.Decimal, error)

// executeFallbackAudit checks every instrument's fallback symbol against the evidence, prints each
// with that evidence, and fails if any instrument is not verified — so it can be run as a check
// after any symbol change (FR-011). It changes nothing: a mapping is only ever stored by migration.
func executeFallbackAudit(ctx context.Context, entries []marketdata.FallbackMappingEvidence, observe observeClose,
	output io.Writer) error {
	counts := map[marketdata.FallbackMappingState]int{}
	for _, entry := range entries {
		if entry.Symbol != "" && entry.StoredClose != nil {
			entry.ObservedCurrency, entry.ObservedClose, entry.ObservedErr = observe(ctx, entry.Symbol, entry.Session)
		}
		state := marketdata.ClassifyFallbackMapping(entry)
		counts[state]++
		observed := "-"
		if entry.ObservedClose != nil {
			observed = entry.ObservedCurrency + " " + entry.ObservedClose.String()
		}
		stored := "-"
		if entry.StoredClose != nil {
			stored = entry.Currency + " " + entry.StoredClose.String() + " on " + entry.Session.String()
		}
		reason := ""
		if entry.ObservedErr != nil {
			reason = " reason=" + marketdata.NormalizeSafeError(marketdata.SanitizeError(entry.ObservedErr.Error())).Code
			var classified *marketdata.ProviderError
			if errors.As(entry.ObservedErr, &classified) {
				reason = " reason=" + classified.Code
			}
		}
		if _, err := fmt.Fprintf(output, "%-10s %s %-12s %-10s stored=%q fallback=%q%s\n", state, entry.MIC,
			entry.Ticker, entry.Symbol, stored, observed, reason); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(output, "verified=%d unmapped=%d mismatched=%d unverified=%d\n",
		counts[marketdata.MappingVerified], counts[marketdata.MappingUnmapped],
		counts[marketdata.MappingMismatched], counts[marketdata.MappingUnverified]); err != nil {
		return err
	}
	if counts[marketdata.MappingVerified] != len(entries) {
		return errors.New("not every instrument's fallback symbol is verified")
	}
	return nil
}

// executeFallbackReconcile asks the primary for every session a fallback price covers, lets its
// bars replace the fallback's (each kept as a revision), reports what changed, and observes the
// state so the banner and the owner hear that the fallback ended (FR-020 – FR-023). Safe to run
// again: what was replaced is no longer a fallback price, so a second run asks only for what is
// left.
func executeFallbackReconcile(ctx context.Context, ops fallbackOperations, importer marketDataImporter,
	differences func(context.Context, instruments.UUID) ([]marketdata.ReconciliationDifference, error),
	output io.Writer, primary, appVersion string, maxRetries, workers int) error {
	targets, err := ops.ReconcileTargets(ctx, primary)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		_, err := fmt.Fprintln(output, "nothing to reconcile: no fallback prices are stored")
		return err
	}
	run, err := importer.Import(ctx, marketdata.ImportRequest{
		Kind: marketdata.ImportBackfill, Provider: primary, AppVersion: appVersion, Targets: targets,
		MaxRetries: maxRetries, Workers: workers,
	})
	if err != nil {
		return err
	}
	if err := writeImportTotals(output, run); err != nil {
		return err
	}
	if differences != nil {
		replaced, err := differences(ctx, run.ID)
		if err != nil {
			return err
		}
		for _, difference := range replaced {
			flag := ""
			if difference.BeyondTolerance {
				flag = " beyond_tolerance"
			}
			if _, err := fmt.Fprintf(output, "replaced %s %s fallback=%s primary=%s%s\n", difference.Ticker,
				difference.Session, difference.FallbackClose, difference.PrimaryClose, flag); err != nil {
				return err
			}
		}
	}
	state, _, err := ops.Observe(ctx)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "active=%t instruments=%d pending_reconciliation=%t\n",
		state.Active, state.Instruments, state.PendingReconciliation)
	return err
}

// fallbackPeriod names a fallback period by the session it began on. An ending's state no longer
// has one, so it is read from what was recorded before the change.
func fallbackPeriod(change marketdata.FallbackChange) string {
	since := change.State.Since
	if change.Ended || since == nil {
		since = change.State.Recorded.Since
	}
	if since == nil {
		return "unknown"
	}
	return since.String()
}

// newFallback builds the fallback service over the Yahoo client, telling the owner of each change
// through the notification outbox in the transaction that records it.
func newFallback(pool *pgxpool.Pool, timeout time.Duration) (*marketdata.Fallback, *yahoo.Client, error) {
	client, err := yahoo.New(yahoo.Config{HTTPClient: &http.Client{Timeout: timeout}})
	if err != nil {
		return nil, nil, err
	}
	repository := marketdata.NewRepository(pool)
	fallback := marketdata.NewFallback(repository, marketdata.NewImportService(repository, client))
	fallback.Tell = func(ctx context.Context, tx pgx.Tx, change marketdata.FallbackChange) error {
		_, err := notify.RaiseFallback(ctx, tx, change.Entered, change.State.Instruments,
			fallbackPeriod(change), time.Now().UTC())
		return err
	}
	return fallback, client, nil
}
