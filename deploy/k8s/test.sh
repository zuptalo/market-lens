#!/usr/bin/env bash
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
required=(
  00-namespace.yaml
  05-traefik-acme.yaml
  10-postgres.yaml
  20-market-lens.yaml
  30-ingress.yaml
  40-keel.yaml
  install.sh
)

for file in "${required[@]}"; do
  test -f "$DIR/$file" || {
    echo "missing deployment file: $file" >&2
    exit 1
  }
done

bash -n "$DIR/install.sh"

grep -Fq 'image: ghcr.io/zuptalo/market-lens:latest' "$DIR/20-market-lens.yaml"
grep -Fq 'imagePullPolicy: Always' "$DIR/20-market-lens.yaml"
grep -Fq 'keel.sh/policy: force' "$DIR/20-market-lens.yaml"
grep -Fq 'keel.sh/trigger: poll' "$DIR/20-market-lens.yaml"
grep -Fq 'keel.sh/pollSchedule: "@every 2m"' "$DIR/20-market-lens.yaml"
grep -Fq 'path: /api/v1/health' "$DIR/20-market-lens.yaml"
grep -Fq 'path: /api/v1/ready' "$DIR/20-market-lens.yaml"
grep -Fq 'runAsUser: 100' "$DIR/20-market-lens.yaml"
grep -Fq 'runAsGroup: 101' "$DIR/20-market-lens.yaml"
grep -Fq 'secretKeyRef:' "$DIR/10-postgres.yaml"
grep -Fq 'secretKeyRef:' "$DIR/20-market-lens.yaml"
if grep -Fq 'drop: [ALL]' "$DIR/10-postgres.yaml"; then
  echo "PostgreSQL entrypoint requires ownership capabilities during initialization" >&2
  exit 1
fi
grep -Fq 'tlschallenge=true' "$DIR/05-traefik-acme.yaml"
grep -Fq 'router.tls.certresolver: letsencrypt' "$DIR/30-ingress.yaml"
grep -Fq 'entrypoints: web' "$DIR/30-ingress.yaml"
grep -Fq 'scheme: https' "$DIR/30-ingress.yaml"

if command -v kubectl >/dev/null && command -v envsubst >/dev/null; then
  rendered="$(mktemp -d)"
  trap 'rm -rf "$rendered"' EXIT
  export APP_HOST=market-lens.example.test ACME_EMAIL=operator@example.test
  bundle="$rendered/all.yaml"
  for file in 00-namespace 05-traefik-acme 10-postgres 20-market-lens 30-ingress 40-keel; do
    envsubst '${APP_HOST} ${ACME_EMAIL}' < "$DIR/$file.yaml" >> "$bundle"
    printf '\n---\n' >> "$bundle"
  done
  kubectl apply --dry-run=client --validate=false -f "$bundle" >/dev/null
fi

echo "k3s manifest contract: ok"
