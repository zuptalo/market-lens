# Quickstart: Release Versioning and Protected Delivery

## Local policy checks

```sh
scripts/release-version.test.sh
scripts/workflow-contract.test.sh
scripts/release-version.sh validate-branch 003-release-versioning
scripts/release-version.sh next v0.1.0 'feat(release): automate versioned delivery'
```

The final command must print `0.2.0`.

## Local build identity

```sh
APP_VERSION=0.2.0 npm run build
docker build --build-arg VERSION=0.2.0 -t market-lens:0.2.0 .
```

The browser shell shows `v0.2.0`; `/api/v1/health` returns `"version":"0.2.0"` from the
container. Plain `npm run dev` displays `development`.

## Repository configuration verification

```sh
gh repo view zuptalo/market-lens --json mergeCommitAllowed,rebaseMergeAllowed,squashMergeAllowed,deleteBranchOnMerge
gh api repos/zuptalo/market-lens/rulesets
gh release view v0.1.0 --repo zuptalo/market-lens
```

Expected: squash only, automatic branch deletion, active main ruleset with no bypass,
and foundation baseline release present.

## Delivery verification

Open a PR titled `feat(release): automate versioned delivery`. After checks pass, squash
merge it. Confirm the release workflow succeeds, `v0.2.0` exists, all required GHCR tags
reference the published digest, and the deployed application reports/displays `0.2.0`.

## Full local verification

```sh
make verify
npm run test:e2e
docker build --build-arg VERSION=0.2.0 -t market-lens:0.2.0 .
docker compose config
deploy/k8s/test.sh
```

