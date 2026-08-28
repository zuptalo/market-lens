# GitHub delivery, releases, and sensitive configuration

## Pull-request validation

`Market Lens CI` runs for every pull request to `main` without path filters. The stable
`Required checks` gate succeeds only when PR policy, frontend, backend, Playwright, and
container validation all succeed. Repository rules require that aggregate check from the
GitHub Actions app, an up-to-date branch, a pull request, resolved conversations, and a
squash merge. Administrators have no bypass.

Branches use `NNN-lowercase-kebab`. Pull-request titles use an allowed Conventional
Commit type; the title becomes the squash commit subject and release input.

## Semantic versioning

The greatest `vMAJOR.MINOR.PATCH` Git tag is the version source. `v0.1.0` records the
foundation baseline. Each successful merge produces another release:

- a title containing the conventional `!` breaking marker increments major;
- `feat` increments minor;
- `fix`, `perf`, `refactor`, `docs`, `test`, `build`, `ci`, `chore`, and `revert`
  increment patch.

Do not edit `package.json` or another source file to release and do not create normal
application release tags manually. `scripts/release-version.sh` is the tested policy.

## Release workflow

`Market Lens Release` runs after a squash merge pushes to protected `main`. Release runs
are serialized and never cancel or replace a pending merge. A run:

1. validates the squash title and calculates or resumes the version for its exact SHA;
2. atomically reserves the semantic tag and creates/resumes a draft release;
3. reruns repository, Playwright, Compose, and k3s verification;
4. builds one `linux/amd64` and `linux/arm64` image with the same version compiled into
   the Go API and Vue shell;
5. publishes and verifies image tags, SBOM, and provenance attestation;
6. publishes the draft as an immutable GitHub release last.

An already published release for the same SHA is a successful no-op. A failed run leaves
no completed release; rerunning resumes its tag/draft. Conflicting tags fail and never
move.

For version `0.2.0`, the same GHCR digest is available as:

```text
ghcr.io/zuptalo/market-lens:0.2.0
ghcr.io/zuptalo/market-lens:0.2
ghcr.io/zuptalo/market-lens:0
ghcr.io/zuptalo/market-lens:sha-<full-commit-sha>
ghcr.io/zuptalo/market-lens:latest
```

Full SemVer and commit tags are diagnostic identities. Major/minor/`latest` are moving
aliases. The optional Keel deployment continues following `latest`.

## Confirming a deployment

The shell visibly displays `vMAJOR.MINOR.PATCH`, and `GET /api/v1/health` returns the
same value without `v`. Local builds display `development`. Compare the running value
with the GitHub Release and GHCR digest rather than inferring it from deployment time.

## Variables, tokens, and secrets

Publishing uses only GitHub's short-lived `GITHUB_TOKEN` with job-scoped contents,
packages, attestation, and OIDC permissions. No managed GHCR credential is required.

- Store non-sensitive configuration in Actions variables as `${{ vars.NAME }}`.
- Store sensitive values only in repository/environment secrets as
  `${{ secrets.NAME }}`.
- Never put secrets in workflow source, variables, build arguments, images, logs,
  Compose files, or examples.
- Prefer environment-scoped secrets for future deployments and pass jobs only what they
  require.

Example files contain development placeholders. Real deployment configuration is
runtime-only and never baked into the image.
