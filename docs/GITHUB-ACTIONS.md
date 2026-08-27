# GitHub Actions and sensitive configuration

The `Market Lens CI` workflow validates the backend, frontend, Playwright smoke test,
Compose configuration, and container build. Pull requests build without publishing.
Successful pushes to `main` publish a multi-platform image to:

```text
ghcr.io/zuptalo/market-lens:latest
ghcr.io/zuptalo/market-lens:main
ghcr.io/zuptalo/market-lens:sha-<commit>
```

The image is built for `linux/amd64` and `linux/arm64`, includes an SBOM and provenance,
and is authenticated with GitHub's short-lived `GITHUB_TOKEN`. No manually managed GHCR
credential is required.

## Variables and secrets

- Store non-sensitive configuration in GitHub Actions **repository or environment
  variables** and access it as `${{ vars.NAME }}`.
- Store passwords, tokens, private keys, credentials, and all other sensitive values in
  GitHub Actions **repository or environment secrets** and access them as
  `${{ secrets.NAME }}`.
- Never put sensitive values in workflow YAML, source files, Docker build arguments,
  Compose files, example environment files, logs, or repository variables. GitHub
  variables are not secret.
- Prefer environment-scoped secrets for future deployments so protection and approval
  rules can be applied independently of build and test jobs.
- Do not pass a secret to a job or step that does not require it.

Example files contain development-only placeholders. Real deployment configuration must
be supplied at runtime; it must not be baked into the container image.
