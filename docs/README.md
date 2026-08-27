# Developer notes

Market Lens is a modular Go application with a Vue client. In development Vite proxies
`/api` to the Go server. In production the Docker build copies Vite's `dist/` into the
runtime image and `STATIC_DIR` enables the Go SPA handler.

The baseline migration creates only the migration ledger. Financial entities belong to
future feature specifications. Background jobs should remain inside the Go process and
be tied to application context cancellation and graceful shutdown.
