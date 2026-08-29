#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
PORT_FILES=$(mktemp -d)
PIDS=()

cleanup() {
  for pid in "${PIDS[@]:-}"; do
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  done
  rm -rf "$PORT_FILES"
}
trap cleanup EXIT

start_listener() {
  local output=$1
  node -e 'const net=require("node:net"); const server=net.createServer(); server.listen(0,"127.0.0.1",()=>console.log(server.address().port));' >"$output" &
  PIDS+=("$!")
  for _ in {1..50}; do
    [ -s "$output" ] && return
    sleep 0.02
  done
  echo "listener did not start" >&2
  exit 1
}

start_listener "$PORT_FILES/one"
start_listener "$PORT_FILES/two"
port_one=$(<"$PORT_FILES/one")
port_two=$(<"$PORT_FILES/two")

"$ROOT/scripts/dev-ports.sh" "$port_one" "$port_two"

for pid in "${PIDS[@]}"; do
  if kill -0 "$pid" 2>/dev/null; then
    echo "listener $pid survived port cleanup" >&2
    exit 1
  fi
done

"$ROOT/scripts/dev-ports.sh" "$port_one" "$port_two"

if "$ROOT/scripts/dev-ports.sh" invalid >/dev/null 2>&1; then
  echo "invalid port was accepted" >&2
  exit 1
fi

if ! make -n -C "$ROOT" start | grep -E 'scripts/dev-ports\.sh .*5173' >/dev/null; then
  echo "make start does not invoke development port cleanup" >&2
  exit 1
fi

echo "development port cleanup tests passed"
