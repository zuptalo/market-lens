# Quickstart: Instruments and Daily Market Data

This is the planned developer/acceptance workflow. Never commit the token or use it as a
Docker build argument.

## Configuration

```sh
export MARKET_DATA_PROVIDER=eodhd
export EODHD_API_TOKEN='<personal token>'
export MARKET_DATA_SCHEDULE_ENABLED=false
export MARKET_DATA_DAILY_TIME=20:00
export MARKET_DATA_DAILY_TIMEZONE=Europe/Stockholm
```

Provider operations fail closed without a token. Read-only views continue to show stored
data and a sanitized configuration status.

## Focused verification

```sh
make db-up
cd server && go test ./internal/instruments ./internal/marketdata ./internal/db
cd .. && npm run test:unit
```

Integration tests use a disposable database and deterministic fixture provider; they do
not call EODHD or depend on current prices.

## Load and retry history

```sh
cd server
go run ./cmd/market-lens marketdata backfill --universe nordic-liquid-v1 --years 10
```

In Compose:

```sh
docker compose exec app /app/market-lens marketdata backfill --universe nordic-liquid-v1 --years 10
docker compose exec app /app/market-lens marketdata retry --run '<run-uuid>'
```

Commands print only run IDs and safe totals. Detailed status is read-only in the UI/API.

## Acceptance inspection

1. Open `/markets`; search 50–100 listings by ticker, name, and ISIN with exchange and
   currency visible.
2. Open an instrument; verify latest session/native OHLCV, adjustment status, coverage,
   freshness, and warnings.
3. Review run/item counts and sanitized failures in market-data status.
4. Repeat the backfill; verify no duplicates and unchanged visible values.
5. Import a fixture correction; verify one revision and the corrected current bar.

## Full verification

```sh
make verify
npm run test:e2e
docker build -t market-lens:local .
docker compose config
```

Playwright covers 360x800, 768x1024, and 1440x900; a 320x800 assertion rejects accidental
page-level horizontal overflow.
