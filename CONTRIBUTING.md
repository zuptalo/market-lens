# Contributing to Market Lens

Market Lens uses specification-driven development. Create or update a document under
`specs/` before implementing a substantial feature, and keep the specification, tests,
and documentation aligned with the resulting behavior.

## Branches and pull requests

Never work directly on `main`. Create one branch per reviewed specification using its
three-digit identifier and lowercase kebab-case name, for example:

```sh
git switch main
git pull --ff-only
git switch -c 002-instruments-market-data
```

Pull-request titles use Conventional Commit form and become the single squash commit on
`main`. Allowed types are `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`,
`ci`, `chore`, and `revert`, with an optional lowercase scope and `!` breaking marker:

```text
feat(market-data): ingest daily Nordic bars
fix(api): preserve exchange-qualified identity
feat(strategy)!: replace the signal contract
```

GitHub blocks nonconforming branches/titles, direct/force pushes, unresolved review
threads, and pull requests whose required checks fail. Squash is the only merge method;
merged feature branches are deleted automatically. The repository requires zero
approvals so its single maintainer can merge, but the PR/check/conversation gates apply
to administrators too.

Every merged pull request releases automatically after post-merge verification. A
breaking `!` increments major, `feat` increments minor, and every other allowed type
increments patch. Do not edit a source version or create application release tags by
hand; Git tags and release automation are authoritative.

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

All user-facing changes are mobile-first. Specifications and reviews must describe and
verify mobile, tablet, and desktop behavior, including representative 360x800, 768x1024,
and 1440x900 Playwright viewports. Layouts must tolerate 320 CSS pixels without
accidental page scrolling, clipped controls, or hover-only interactions. Give tables,
charts, dialogs, menus, and navigation an intentional small-screen design.

Never commit sensitive configuration. GitHub Actions secrets are required for sensitive
CI/CD values; Actions variables are only for non-sensitive configuration. Do not put
credentials in workflow YAML, build arguments, images, logs, or example files.
