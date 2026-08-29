#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: $0 PORT [PORT ...]" >&2
  exit 2
fi

for port in "$@"; do
  if ! [[ "$port" =~ ^[0-9]+$ ]] || [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
    echo "invalid port: $port" >&2
    exit 2
  fi

  pids=$(lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)
  [ -n "$pids" ] || continue

  echo "Stopping process(es) using development port $port: ${pids//$'\n'/ }"
  for pid in $pids; do
    if [ "$pid" != "$$" ] && [ "$pid" != "$PPID" ]; then
      kill -TERM "$pid" 2>/dev/null || true
    fi
  done

  for _ in {1..20}; do
    remaining=$(lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)
    [ -z "$remaining" ] && break
    sleep 0.1
  done

  remaining=$(lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)
  if [ -n "$remaining" ]; then
    echo "Force-stopping process(es) still using development port $port: ${remaining//$'\n'/ }"
    for pid in $remaining; do
      if [ "$pid" != "$$" ] && [ "$pid" != "$PPID" ]; then
        kill -KILL "$pid" 2>/dev/null || true
      fi
    done
  fi
done
