#!/usr/bin/env bash
# Idempotent k3s installer for Market Lens. Secrets are generated once and kept
# on subsequent runs; this script never performs SQL or direct DB manipulation.
set -euo pipefail

: "${APP_HOST:=market-lens.zuptalo.com}"
: "${ACME_EMAIL:?set ACME_EMAIL, e.g. ACME_EMAIL=you@example.com}"
KUBECTL="${KUBECTL:-kubectl}"
DIR="$(cd "$(dirname "$0")" && pwd)"
export APP_HOST ACME_EMAIL

command -v "$KUBECTL" >/dev/null || { echo "kubectl is required" >&2; exit 1; }
command -v envsubst >/dev/null || { echo "envsubst (gettext) is required" >&2; exit 1; }
command -v openssl >/dev/null || { echo "openssl is required" >&2; exit 1; }

echo "==> namespace"
"$KUBECTL" create namespace market-lens --dry-run=client -o yaml | "$KUBECTL" apply -f -

if "$KUBECTL" -n market-lens get secret market-lens-secrets >/dev/null 2>&1; then
  echo "==> market-lens-secrets exists; preserving credentials"
  if [[ -z "$("$KUBECTL" -n market-lens get secret market-lens-secrets -o jsonpath='{.data.AUTH_SECRET}')" ]]; then
    echo "==> adding authentication key to existing secret"
    auth_secret="$(openssl rand -base64 48)"
    "$KUBECTL" -n market-lens patch secret market-lens-secrets --type=merge \
      --patch "{\"stringData\":{\"AUTH_SECRET\":\"$auth_secret\"}}" >/dev/null
  fi
  external_key_data="$("$KUBECTL" -n market-lens get secret market-lens-secrets -o jsonpath='{.data.EXTERNAL_CREDENTIAL_KEY}')"
  external_version_data="$("$KUBECTL" -n market-lens get secret market-lens-secrets -o jsonpath='{.data.EXTERNAL_CREDENTIAL_KEY_VERSION}')"
  if [[ -z "$external_key_data" && -z "$external_version_data" ]]; then
    echo "==> adding external credential encryption key to existing secret"
    external_credential_key="$(openssl rand -base64 32)"
    "$KUBECTL" -n market-lens patch secret market-lens-secrets --type=merge \
      --patch "{\"stringData\":{\"EXTERNAL_CREDENTIAL_KEY\":\"$external_credential_key\",\"EXTERNAL_CREDENTIAL_KEY_VERSION\":\"1\"}}" >/dev/null
  elif [[ -z "$external_key_data" || -z "$external_version_data" ]]; then
    echo "market-lens-secrets contains an incomplete external credential key configuration" >&2
    exit 1
  fi
else
  echo "==> generating market-lens-secrets"
  password="$(openssl rand -hex 24)"
  auth_secret="$(openssl rand -base64 48)"
  external_credential_key="$(openssl rand -base64 32)"
  database_url="postgres://market_lens:${password}@market-lens-postgres:5432/market_lens?sslmode=disable"
  "$KUBECTL" -n market-lens create secret generic market-lens-secrets \
    --from-literal=POSTGRES_PASSWORD="$password" \
    --from-literal=DATABASE_URL="$database_url" \
    --from-literal=AUTH_SECRET="$auth_secret" \
    --from-literal=EXTERNAL_CREDENTIAL_KEY="$external_credential_key" \
    --from-literal=EXTERNAL_CREDENTIAL_KEY_VERSION="1"
fi

echo "==> configuring persistent Traefik ACME resolver"
envsubst '${ACME_EMAIL}' < "$DIR/05-traefik-acme.yaml" | "$KUBECTL" apply -f -

echo "==> applying Market Lens for https://${APP_HOST}"
for file in 00-namespace 10-postgres 20-market-lens 30-ingress 40-keel; do
  envsubst '${APP_HOST} ${ACME_EMAIL}' < "$DIR/$file.yaml" | "$KUBECTL" apply -f -
done

# envFrom changes are read only on pod creation. A restart also ensures a rerun
# immediately reconciles the mutable latest image.
"$KUBECTL" -n market-lens rollout restart deployment/market-lens >/dev/null 2>&1 || true

echo "==> waiting for controllers and workloads"
"$KUBECTL" -n kube-system rollout status deployment/traefik --timeout=180s
"$KUBECTL" -n keel rollout status deployment/keel --timeout=120s
"$KUBECTL" -n market-lens rollout status statefulset/market-lens-postgres --timeout=180s
"$KUBECTL" -n market-lens rollout status deployment/market-lens --timeout=180s

cat <<EOF

Market Lens is deployed.
  URL:    https://${APP_HOST}
  Health: https://${APP_HOST}/api/v1/health

Keel polls ghcr.io/zuptalo/market-lens:latest every two minutes.
The first Let's Encrypt TLS-ALPN issuance can take a minute after Traefik is ready.
EOF
