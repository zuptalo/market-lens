# Market Lens Constitution

## Core principles

### I. Specification-driven development

Financial entities, API contracts, calculations, and user workflows must be defined in
a reviewed feature specification before implementation. The foundation must not preempt
decisions belonging to future market-data, signal, backtest, risk, portfolio, or
paper-trading specifications.

No production behavior may be implemented until its governing specification is valid,
reviewed, and contains independently testable acceptance criteria. If implementation
reveals missing or incorrect requirements, update and review the specification before
continuing rather than allowing the code to become the undocumented source of truth.

### II. Modular monolith

The backend remains one Go application and one deployable image. Feature packages have
clear ownership and narrow interfaces. PostgreSQL is the system of record; background
jobs run inside the application and stop with its context.

### III. Migration-only database evolution

All schema changes, seed or reference data, backfills, data corrections, and other
persistent database transformations must be expressed as ordered, reviewable,
transactional migrations. Manual SQL, console edits, ad-hoc database scripts, and direct
database manipulation are prohibited as implementation, deployment, repair, and
production-support mechanisms. Production database access must never be required for a
release or correction.

Applied migrations are immutable. A mistake is corrected with a new forward migration,
and migrations must be exercised by automated tests before release. Normal application
reads and writes are performed through reviewed application code; this rule prohibits
manual operational mutation, not ordinary runtime persistence.

### IV. Explicit, versioned contracts

HTTP APIs use REST/JSON under `/api/v1`. Handlers validate input and return consistent
JSON errors. Contracts remain synchronized with their specifications and tests.

### V. Correctness and reproducibility

Research and backtesting behavior must be deterministic for identical inputs and make
time zones, calendars, missing data, costs, and numerical assumptions explicit. Future
financial calculations require focused tests at their behavioral boundaries.

### VI. Test-driven development

Production code follows strict red-green-refactor development:

1. Write a focused automated test for the specified behavior before production code.
2. Run it and confirm it fails for the expected behavioral reason. A test that already
   passes or fails because of invalid setup is not a valid red test.
3. Write only enough production code to make the test pass.
4. Run the relevant suite, confirm green, and keep it green while refactoring.

No production code may be written or changed without a valid failing test demonstrating
the need for that change. Tests may not be weakened, skipped, deleted, or rewritten just
to make a change pass. If the behavior cannot be tested automatically, implementation
stops until the specification and test strategy provide a valid red test.

Changes also keep Go tests, vet, frontend type checking, Vitest, relevant Playwright
flows, the production build, and container configuration healthy.

### VII. PrimeVue-first accessible and responsive UI

The Vue client prefers PrimeVue primitives and reusable project components. User-facing
surfaces support system, light, and dark themes, keyboard use, semantic structure, and
responsive layouts.

Every user-facing element and workflow must be designed mobile-first and remain fully
usable on mobile, tablet, and desktop. Feature specifications must define responsive
behavior and acceptance criteria before UI implementation. At minimum, automated
browser coverage must exercise representative 360x800 mobile, 768x1024 tablet, and
1440x900 desktop viewports; layouts must also tolerate a 320 CSS-pixel-wide viewport
without unintended horizontal page scrolling, clipped actions, overlapping content, or
inaccessible controls.

Navigation, forms, dialogs, menus, feedback, charts, and data-dense tables must have an
intentional small-screen treatment. Interactions must not depend on hover or precise
pointer input, primary touch targets should be comfortably operable, text must remain
readable without zoom, and orientation or viewport changes must not lose user state.
Horizontal scrolling is permitted only for intrinsically wide content when explicitly
contained and documented; it must never be the accidental behavior of the page shell.

### VIII. Self-hosted operational simplicity

Production uses one Market Lens image plus PostgreSQL. Configuration comes from the
environment, secrets are never committed, logs are structured, readiness reflects
dependency health, and shutdown is graceful. Additional infrastructure requires an
explicit architectural specification.

Sensitive CI/CD values must be supplied only through GitHub Actions secrets; repository
or environment variables are reserved for non-sensitive configuration because they are
not secret. Credentials must never appear in workflow source, logs, build arguments,
container layers, example configuration, or committed files. Jobs receive only the
secrets they require.

## Governance

This constitution guides specification planning and implementation review. Amendments
must update affected templates and project instructions in the same change.

**Version**: 1.3.0
**Ratified**: 2026-08-28  
**Last amended**: 2026-08-28
