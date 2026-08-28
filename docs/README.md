# Developer notes

Durable project context:

- [`product-vision.md`](product-vision.md) preserves long-term scope, architecture,
  milestone intent, and non-goals across sessions.
- [`../ROADMAP.md`](../ROADMAP.md) records delivery status, dependencies, and next work.
- [`../specs/README.md`](../specs/README.md) indexes existing and anticipated feature
  specifications.

Market Lens is a modular Go application with a Vue client. In development Vite proxies
`/api` to the Go server. In production the Docker build copies Vite's `dist/` into the
runtime image and `STATIC_DIR` enables the Go SPA handler.

The baseline migration creates only the migration ledger. Financial entities belong to
future feature specifications. Background jobs should remain inside the Go process and
be tied to application context cancellation and graceful shutdown.
