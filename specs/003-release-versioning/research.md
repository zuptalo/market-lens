# Research: Release Versioning and Protected Delivery

## Decision 1: Repository ruleset plus squash-only repository settings

**Decision**: Use an active branch ruleset targeting `main`, with mandatory pull request,
zero approvals, resolved conversations, required checks, linear history, and force-push/
deletion protection. Configure the repository to allow only squash merge, use the PR
title as commit title, delete merged branches, and apply no bypass actors.

**Rationale**: GitHub supports required pull requests with zero approvals, which preserves
the PR record without blocking a single maintainer. Rulesets apply to administrators when
there is no bypass. Squash-only settings align the validated title, release classification,
and linear main history.

Primary sources: [ruleset rules](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets)
and [repository settings API](https://docs.github.com/en/rest/repos/repos).

**Alternatives considered**: One approval makes self-authored work unmergeable; classic
branch protection is less inspectable as a named policy; merge/rebase methods weaken the
one-PR/one-release mapping.

## Decision 2: Conventional PR title drives every release

**Decision**: Accept `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`, `ci`,
`chore`, and `revert`, with optional lowercase scope and optional `!`. Breaking titles
bump major, `feat` bumps minor, and all other accepted types bump patch.

**Rationale**: The squash commit preserves the PR title, so one validated input controls
both readable history and deterministic release allocation. Every merged PR builds a
deployable image, so even docs/CI changes get traceable patch versions.

**Alternatives considered**: Labels can be forgotten or changed at merge time;
release-please adds a second release PR and does not satisfy immediate release-on-merge;
manually edited version files require direct follow-up commits.

## Decision 3: Tags are the version source

**Decision**: Calculate from the numerically greatest reachable `vMAJOR.MINOR.PATCH` tag,
using `v0.1.0` as the explicit foundation baseline. Do not use `package.json` as the
application release source.

**Rationale**: A tag identifies the immutable source commit and avoids an automated
version-bump commit on protected main. Numeric sorting avoids lexical `v0.10.0` errors.

**Alternatives considered**: Source version files create loops or extra PRs; run numbers
are not semantic and cannot communicate change impact.

## Decision 4: Dedicated serialized release workflow on main push

**Decision**: PR CI validates every suite and exposes one stable aggregate check. A
separate push-to-main workflow uses a non-cancelling maximum queue, validates/resumes the
version for the exact SHA, reserves its tag and draft, performs post-merge verification,
publishes one digest, attests it, and publishes the release last. Reruns of a published
SHA are successful no-ops.

**Rationale**: Direct push is blocked, so main pushes are squash merges. Serialization
prevents two events allocating the same tag. Creating the release last avoids claiming
success before image publication.

Primary sources: [workflow syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax)
and [status checks](https://docs.github.com/en/pull-requests/reference/status-checks).

**Alternatives considered**: `workflow_run` grants a privileged follow-up workflow and
adds indirection; publishing inside PR CI risks credentials on fork events; release on a
second manual event violates automatic shipping.

## Decision 5: Native immutable releases and one digest with SemVer aliases

**Decision**: Enable native immutable releases, stage each as a draft, and publish only
after verification. Publish `X.Y.Z`, `X.Y`, `X`, `sha-<full>`, and `latest` for one
amd64/arm64 digest, with SBOM/provenance. Use only least-privilege `GITHUB_TOKEN`.

**Rationale**: Full and commit tags are diagnostic/immutable; aliases support controlled
compatibility and existing Keel deployment behavior. One digest prevents tag variants
from containing different builds.

Primary sources: [immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases)
and [Docker metadata action](https://docs.docker.com/build/ci/github-actions/manage-tags-labels/).

**Alternatives considered**: `latest` alone is ambiguous; separate rebuilds per tag can
diverge; an external registry token is unnecessary.

## Decision 6: Compile-time shared build identity

**Decision**: Pass one normalized version build argument to both Docker build stages.
Go receives it through existing linker flags; Vite exposes a compile-time constant. Local
builds default to `dev`. The shell renders `vX.Y.Z` or `development`; health returns the
unprefixed identity.

**Rationale**: No network request or extra runtime configuration is needed, and frontend/
backend come from the same build invocation.

**Alternatives considered**: Reading package metadata can drift; fetching health to show
the version adds failure/loading behavior; a generated committed file causes dirty trees.
