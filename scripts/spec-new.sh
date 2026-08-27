#!/usr/bin/env bash
set -euo pipefail

description="${1:-}"
if [[ -z "$description" ]]; then
  echo 'Usage: scripts/spec-new.sh "Feature description"' >&2
  exit 2
fi

exec .specify/scripts/bash/create-new-feature.sh --json "$description"
