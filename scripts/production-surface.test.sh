#!/usr/bin/env bash
# Exercises the production artifact: the built image serving the built client through the Go
# router, which is the combination nothing else tests. The browser suite serves the client
# through `vite preview`, whose index.html fallback answers every path, so it cannot see how
# the real server routes an unauthenticated visitor.
set -euo pipefail

IMAGE=${IMAGE:-market-lens:surface-test}
NETWORK=market-lens-surface-$$
DB=market-lens-surface-db-$$
APP=market-lens-surface-app-$$
MINIMAL=market-lens-surface-minimal-$$
REKEYED=market-lens-surface-rekeyed-$$
PORT=${PORT:-18080}
MINIMAL_PORT=$((${PORT:-18080} + 1))
FAILURES=0

cleanup() {
  docker rm -f "$APP" "$MINIMAL" "$REKEYED" "$DB" >/dev/null 2>&1 || true
  docker network rm "$NETWORK" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# check NAME EXPECTED ACTUAL records a failure without stopping, so one run reports every
# broken surface rather than only the first.
check() {
  local name=$1 expected=$2 actual=$3
  if [[ "$actual" == "$expected" ]]; then
    printf '  ok   %-58s %s\n' "$name" "$actual"
  else
    printf '  FAIL %-58s got %s, want %s\n' "$name" "$actual" "$expected"
    FAILURES=$((FAILURES + 1))
  fi
}

status() { curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$@"; }
location() { curl -s -o /dev/null -w '%{redirect_url}' --max-time 10 "$@"; }
body() { curl -s --max-time 10 "$@"; }

echo "==> starting PostgreSQL"
docker network create "$NETWORK" >/dev/null
docker run -d --name "$DB" --network "$NETWORK" \
  -e POSTGRES_USER=market_lens -e POSTGRES_PASSWORD=surface \
  -e POSTGRES_DB=market_lens postgres:18 >/dev/null
for _ in $(seq 1 60); do
  docker exec "$DB" pg_isready -U market_lens -d market_lens >/dev/null 2>&1 && break
  sleep 1
done

echo "==> starting $IMAGE"
docker run -d --name "$APP" --network "$NETWORK" -p "$PORT:8080" \
  -e ENV=production -e PORT=8080 -e STATIC_DIR=/app/web \
  -e "ALLOWED_ORIGINS=http://127.0.0.1:$PORT" \
  -e "DATABASE_URL=postgres://market_lens:surface@$DB:5432/market_lens?sslmode=disable" \
  -e "AUTH_SECRET=$(openssl rand -base64 48)" \
  -e "EXTERNAL_CREDENTIAL_KEY=$(openssl rand -base64 32)" \
  -e EXTERNAL_CREDENTIAL_KEY_VERSION=1 \
  -e AUTH_SECURE_COOKIES=true \
  "$IMAGE" >/dev/null

BASE="http://127.0.0.1:$PORT"
for _ in $(seq 1 60); do
  [[ "$(status "$BASE/api/v1/health")" == "200" ]] && break
  sleep 1
done
if [[ "$(status "$BASE/api/v1/health")" != "200" ]]; then
  echo "the application never became healthy; logs follow" >&2
  docker logs "$APP" 2>&1 | tail -20 >&2
  exit 1
fi

HTML='Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8'

echo "==> public surface"
check "GET /api/v1/health" 200 "$(status "$BASE/api/v1/health")"
check "GET /api/v1/ready" 200 "$(status "$BASE/api/v1/ready")"
check "GET /api/v1/setup/status" 200 "$(status "$BASE/api/v1/setup/status")"
check "GET /login serves the shell" 200 "$(status -H "$HTML" "$BASE/login")"
check "GET /setup serves the shell" 200 "$(status -H "$HTML" "$BASE/setup")"
check "GET /favicon.svg" 200 "$(status "$BASE/favicon.svg")"

echo "==> a person opening the application reaches sign-in"
# The regression this exists to prevent: the bare domain answering a browser with JSON.
for path in / /markets /account; do
  check "browser GET $path redirects" 302 "$(status -H "$HTML" "$BASE$path")"
  check "browser GET $path targets sign-in" "$BASE/login?redirect=$(python3 -c "
import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1], safe=''))" "$path")" \
    "$(location -H "$HTML" "$BASE$path")"
done

echo "==> data stays refused whatever the caller claims to accept"
for path in /api/v1/instruments /api/v1/market-data/imports /api/v1/events /api/v1/account /api/v1/owner/members; do
  check "browser-shaped GET $path" 401 "$(status -H "$HTML" "$BASE$path")"
  check "api GET $path" 401 "$(status "$BASE$path")"
done
check "non-browser GET / refuses" 401 "$(status "$BASE/")"

echo "==> the shell that is served carries the client, not application data"
SHELL_BODY=$(body -H "$HTML" "$BASE/login")
case "$SHELL_BODY" in
  *"<script"*) printf '  ok   %-58s\n' "sign-in shell loads the client" ;;
  *) printf '  FAIL %-58s\n' "sign-in shell loads the client"; FAILURES=$((FAILURES + 1)) ;;
esac

echo "==> setup is open on a fresh database"
case "$(body "$BASE/api/v1/setup/status")" in
  *'"setup_required":true'*) printf '  ok   %-58s\n' "fresh install reports setup required" ;;
  *) printf '  FAIL %-58s\n' "fresh install reports setup required"; FAILURES=$((FAILURES + 1)) ;;
esac

# Feature 009: a production deployment needs only DATABASE_URL. This is the only check that
# runs the real image with nothing else configured, so it is the only thing that would catch a
# start-up requirement creeping back in.
echo "==> a deployment configured with nothing but DATABASE_URL"
docker exec "$DB" createdb -U market_lens market_lens_minimal >/dev/null
MINIMAL_URL="postgres://market_lens:surface@$DB:5432/market_lens_minimal?sslmode=disable"
docker run -d --name "$MINIMAL" --network "$NETWORK" -p "$MINIMAL_PORT:8080" \
  -e "DATABASE_URL=$MINIMAL_URL" "$IMAGE" >/dev/null

MINIMAL_BASE="http://127.0.0.1:$MINIMAL_PORT"
for _ in $(seq 1 60); do
  [[ "$(status "$MINIMAL_BASE/api/v1/health")" == "200" ]] && break
  sleep 1
done
check "minimal deployment becomes healthy" 200 "$(status "$MINIMAL_BASE/api/v1/health")"
check "minimal deployment reports ready" 200 "$(status "$MINIMAL_BASE/api/v1/ready")"
check "minimal deployment sends a browser to sign-in" 302 "$(status -H "$HTML" "$MINIMAL_BASE/")"
case "$(body "$MINIMAL_BASE/api/v1/setup/status")" in
  *'"setup_required":true'*) printf '  ok   %-58s\n' "minimal deployment opens setup" ;;
  *) printf '  FAIL %-58s\n' "minimal deployment opens setup"; FAILURES=$((FAILURES + 1)) ;;
esac

# It must say what it provisioned, and never what the key is.
MINIMAL_LOGS=$(docker logs "$MINIMAL" 2>&1)
# Production logs are JSON, so match the structured field rather than a text-handler pair.
case "$MINIMAL_LOGS" in
  *'"signing_key":"provisioned"'*) printf '  ok   %-58s\n' "minimal deployment reports a provisioned key" ;;
  *) printf '  FAIL %-58s\n' "minimal deployment reports a provisioned key"; FAILURES=$((FAILURES + 1)) ;;
esac
case "$MINIMAL_LOGS" in
  *'key_material'*|*'AUTH_SECRET='*) printf '  FAIL %-58s\n' "minimal deployment logs no key"; FAILURES=$((FAILURES + 1)) ;;
  *) printf '  ok   %-58s\n' "minimal deployment logs no key" ;;
esac

# A restart must reuse the stored key rather than mint a new one, or every session issued
# before the restart would silently stop working - the failure this feature removes.
docker restart "$MINIMAL" >/dev/null
for _ in $(seq 1 60); do
  [[ "$(status "$MINIMAL_BASE/api/v1/health")" == "200" ]] && break
  sleep 1
done
check "minimal deployment restarts healthy" 200 "$(status "$MINIMAL_BASE/api/v1/health")"
case "$(docker logs "$MINIMAL" 2>&1 | tail -40)" in
  *'"signing_key_generation":1'*) printf '  ok   %-58s\n' "restart reuses the provisioned key" ;;
  *) printf '  FAIL %-58s\n' "restart reuses the provisioned key"; FAILURES=$((FAILURES + 1)) ;;
esac

# The other half of the guarantee: an installation started with AUTH_SECRET must refuse to
# come up without it rather than quietly re-key itself and sign everybody out. The first
# database above was started with one, so it is exactly that case.
echo "==> an installation started with AUTH_SECRET refuses to lose it"
docker run -d --name "$REKEYED" --network "$NETWORK" \
  -e "DATABASE_URL=postgres://market_lens:surface@$DB:5432/market_lens?sslmode=disable" \
  "$IMAGE" >/dev/null
sleep 5
REKEYED_STATE=$(docker inspect -f '{{.State.Running}}' "$REKEYED" 2>/dev/null || echo unknown)
check "a removed AUTH_SECRET stops the start" false "$REKEYED_STATE"
case "$(docker logs "$REKEYED" 2>&1)" in
  *'AUTH_SECRET'*) printf '  ok   %-58s\n' "the refusal names AUTH_SECRET" ;;
  *) printf '  FAIL %-58s\n' "the refusal names AUTH_SECRET"; FAILURES=$((FAILURES + 1)) ;;
esac

if [[ "$FAILURES" -gt 0 ]]; then
  echo "production surface: $FAILURES check(s) failed" >&2
  exit 1
fi
echo "production surface: ok"
