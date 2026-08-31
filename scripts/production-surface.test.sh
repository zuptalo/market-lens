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
PORT=${PORT:-18080}
FAILURES=0

cleanup() {
  docker rm -f "$APP" "$DB" >/dev/null 2>&1 || true
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

if [[ "$FAILURES" -gt 0 ]]; then
  echo "production surface: $FAILURES check(s) failed" >&2
  exit 1
fi
echo "production surface: ok"
