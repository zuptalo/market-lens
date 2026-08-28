# Release Policy Contract

## Branch and pull-request grammar

Feature branch:

```text
^[0-9]{3}-[a-z0-9]+(?:-[a-z0-9]+)*$
```

Pull-request/squash title:

```text
^(feat|fix|perf|refactor|docs|test|build|ci|chore|revert)(\([a-z0-9-]+\))?!?: .+$
```

Classification:

- `!` after type/scope: `major`
- `feat` without `!`: `minor`
- every other allowed type: `patch`

## Version command

```text
scripts/release-version.sh bump <title>
scripts/release-version.sh next <latest-tag-or-empty> <title>
scripts/release-version.sh validate-branch <branch>
```

Successful `bump` prints exactly `major`, `minor`, or `patch`. Successful `next` prints
exactly `X.Y.Z`, using `0.1.0` as the no-tag baseline before applying the requested bump
only when a prior tag exists. Invalid input exits non-zero with a safe actionable error.

## Required PR check

The ruleset requires the stable GitHub Actions check `Required checks`. That aggregate
fails unless `PR policy`, `Frontend`, `Backend`, `End-to-end`, and
`Container validation` all succeed.

All run for every pull request to `main`; none uses path filtering.

## Image tags

For version `0.2.0` and SHA `abcdef...`, all reference one digest:

```text
ghcr.io/zuptalo/market-lens:0.2.0
ghcr.io/zuptalo/market-lens:0.2
ghcr.io/zuptalo/market-lens:0
ghcr.io/zuptalo/market-lens:sha-<full-commit-sha>
ghcr.io/zuptalo/market-lens:latest
```

## Runtime identity

`GET /api/v1/health` retains its existing JSON fields and returns unprefixed `version`.
The global shell renders released values with `v` and renders missing/`dev` as
`development`.
