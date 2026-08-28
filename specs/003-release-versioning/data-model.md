# Data Model: Release Versioning and Protected Delivery

This feature has no database model. Its durable identities and transitions are:

## PullRequestChange

Fields: number, head branch, validated conventional title, base branch, head SHA, required
check conclusions, unresolved-conversation count, and merge state.

Validation: head branch matches `NNN-lowercase-kebab`; base is `main`; title has an
allowed type, optional scope/`!`, and non-empty description. Transition:
`open → checks passing → mergeable → squash merged`.

## ReleaseVersion

Fields: major, minor, patch, canonical `X.Y.Z`, tag `vX.Y.Z`, bump kind, source title,
source SHA, and previous tag. Numeric components are non-negative integers with no
leading sign. Exactly one next version follows one previous version and bump kind.

## ReleaseArtifact

Fields: source SHA, image name, digest, platforms, full/minor/major/commit/latest tags,
SBOM/provenance identity, and publication result. All tags from one release reference the
same digest. Transition: `calculated → verified → image published → attested → released`.

## BuildIdentity

Fields: raw injected value, normalized backend value, formatted frontend label. Released
input `X.Y.Z` maps to backend `X.Y.Z` and UI `vX.Y.Z`; missing/`dev` maps to development.

## RepositoryPolicy

Fields: target branch, allowed merge methods, required checks, approval count,
conversation resolution, linear-history, force/deletion protection, admin bypass, and
post-merge branch deletion. Expected state is checked from the GitHub API after changes.

