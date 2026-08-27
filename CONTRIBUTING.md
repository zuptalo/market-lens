# Contributing to Market Lens

Market Lens uses specification-driven development. Create or update a document under
`specs/` before implementing a substantial feature, and keep the specification, tests,
and documentation aligned with the resulting behavior.

All production changes use strict red-green-refactor test-driven development. Write the
automated test first, run it and confirm it fails for the expected reason, implement the
minimum change required to make it pass, then refactor only while the suite remains
green. A test that already passes or fails because its setup is broken is not a valid
starting test. Do not weaken or remove tests to make an implementation pass.

## Development checks

Before proposing a change, run:

```sh
make verify
npm run test:e2e
docker compose config
```

Go code must be formatted with `gofmt`. TypeScript runs in strict mode. Prefer focused
unit tests for pure logic and API behavior, with Playwright covering important user
flows. Every schema change, seed/reference-data update, backfill, and data correction
must use an ordered SQL migration and be defined by the relevant feature specification.
Manual SQL, console edits, and ad-hoc database mutation are not supported deployment or
repair procedures. Correct an applied migration with a new forward migration.

Keep the modular monolith boundary: domain packages may depend on shared transport and
database infrastructure, but unrelated feature areas should communicate through narrow
interfaces rather than shared mutable state.

Never commit sensitive configuration. GitHub Actions secrets are required for sensitive
CI/CD values; Actions variables are only for non-sensitive configuration. Do not put
credentials in workflow YAML, build arguments, images, logs, or example files.
