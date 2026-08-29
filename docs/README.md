# Developer notes

Durable project context:

- [`product-vision.md`](product-vision.md) preserves long-term scope, architecture,
  milestone intent, and non-goals across sessions.
- [`../ROADMAP.md`](../ROADMAP.md) records delivery status, dependencies, and next work.
- [`../specs/README.md`](../specs/README.md) indexes existing and anticipated feature
  specifications.
- [`MARKET-DATA.md`](MARKET-DATA.md) documents host-only imports, provider limitations,
  annual exchange-calendar maintenance, and screenshots-free acceptance inspection.

Market Lens is a modular Go application with a Vue client. In development Vite proxies
`/api` to the Go server. In production the Docker build copies Vite's `dist/` into the
runtime image and `STATIC_DIR` enables the Go SPA handler.

Feature 002 adds shared instrument and daily-market-data entities through ordered
migrations. Background jobs remain inside the Go process and are tied to application
context cancellation and graceful shutdown.
