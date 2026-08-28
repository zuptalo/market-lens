# Implementation Plan: Release Versioning and Protected Delivery

**Branch**: `003-release-versioning` | **Date**: 2026-08-28 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/003-release-versioning/spec.md`

## Summary

Introduce a tested conventional-title/SemVer script, PR policy check, protected
squash-only `main`, serialized GitHub-native release workflow, SemVer/commit/latest GHCR
tags, initial `v0.1.0` baseline, shared build identity injected into Go and Vite, and an
accessible global version badge. Keep `GITHUB_TOKEN` as the only publishing credential.

## Technical Context

**Language/Version**: POSIX shell for release policy; GitHub Actions YAML; Go 1.26;
Vue 3.5 and TypeScript 5.6

**Primary Dependencies**: Existing GitHub Actions, GitHub CLI/API, Docker official
Actions, Vite, PrimeVue, and Go linker flags. No release/versioning package is added.

**Storage**: Git tags, GitHub Releases, GHCR image metadata; no database change

**Testing**: Shell policy/contract tests, Vitest, Go API tests, Playwright, `make verify`,
Docker build, Compose and k3s contract validation, and live GitHub rule inspection

**Responsive UI Verification**: Existing Playwright projects verify the version in the
global shell at 360x800, 768x1024, and 1440x900; the narrow scenario uses 320x800 and a
long development identity to reject overflow/clipping.

**Red-Green-Refactor Proof**: Add `scripts/release-version.test.sh` first and observe it
fail because `scripts/release-version.sh` does not exist. Implement only title validation,
bump classification, and next-version calculation. Add an e2e version assertion and
observe missing shell text before adding the shared frontend build identity.

**Database Evolution**: N/A

**Target Platform**: GitHub-hosted repository/Actions/GHCR and the existing Linux
container, with local macOS/Linux development

**Project Type**: Web application plus repository delivery automation

**Performance Goals**: PR policy completes in under 30 seconds; release completes within
20 minutes of merge; version rendering has no additional network request.

**Constraints**: PR-only main including administrators; zero approval requirement for
single maintainer; squash only; every merge releases; latest tag is highest `vX.Y.Z`;
serialized release; no new secret; no direct source-version bump commit; same backend/
frontend version; safe workflow reruns.

**Scale/Scope**: One public repository, one protected branch, one maintainer, one image,
one automatic release per merged PR, and one global version display.

## Constitution Check

*GATE: Passed before research and re-checked after design.*

| Principle | Evidence | Result |
|---|---|---|
| Specification-driven | `spec.md` defines delivery and visible-version behavior before implementation. | PASS |
| Modular monolith | Version identity is injected into the existing Go/Vue image; no service is added. | PASS |
| Migration-only evolution | No persistent application data changes. | PASS |
| Versioned contracts | Existing health response retains its contract and exact release value is tested. | PASS |
| Correctness/reproducibility | Version parsing, bump rules, tag source, concurrency, and artifact identity are deterministic. | PASS |
| Test-driven development | Shell and browser red tests precede implementation; all repository checks remain required. | PASS |
| Responsive accessible UI | Global text badge has explicit mobile/tablet/desktop/320 and theme coverage. | PASS |
| Operational simplicity | GitHub-native release and repository token only; no new infrastructure or credential. | PASS |

Post-design result: PASS. Repository rules are external configuration but have a checked
declarative contract and are verified live with `gh` after application.

## Project Structure

### Documentation (this feature)

```text
specs/003-release-versioning/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/release-policy.md
├── checklists/requirements.md
└── tasks.md
```

### Source Code (repository root)

```text
.github/
├── PULL_REQUEST_TEMPLATE.md
└── workflows/
    ├── ci.yml
    └── release.yml
scripts/
├── release-version.sh
├── release-version.test.sh
└── workflow-contract.test.sh
src/
├── components/AppShell.vue
├── styles/main.css
├── utils/version.ts
├── utils/version.test.ts
└── vite-env.d.ts
e2e/smoke.spec.ts
Dockerfile
vite.config.ts
Makefile
CONTRIBUTING.md
docs/GITHUB-ACTIONS.md
```

**Structure Decision**: Keep release policy as a small repository script callable both
by tests and Actions. Keep image publishing isolated from PR validation. Build identity
is compile-time data; the browser makes no runtime version request.

## Implementation Sequence

1. Write and run the failing release-version test; implement the minimum policy script.
2. Add workflow contract tests, PR policy job, and release workflow.
3. Add failing frontend/e2e version expectations; inject and render the build identity.
4. Update contributor, release, roadmap, and spec-registry documentation.
5. Run local verification and container checks; commit and push the feature branch.
6. Configure repository merge settings and protected `main` rules through `gh`; create
   the `v0.1.0` foundation baseline; inspect settings.
7. Open a conventionally titled PR, wait for required checks, squash merge, observe the
   automatic `v0.2.0` release/image, and verify the live repository state.
8. Restore the active Spec Kit pointer to feature 002 and generate its tasks on a new
   `002-instruments-market-data` branch.

## Complexity Tracking

No constitution violations require justification.

