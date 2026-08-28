#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SCRIPT="$ROOT/scripts/release-version.sh"

if [[ ! -x "$SCRIPT" ]]; then
  echo "expected executable release policy script at $SCRIPT" >&2
  exit 1
fi

assert_output() {
  local expected=$1
  shift
  local actual
  actual=$($SCRIPT "$@")
  if [[ "$actual" != "$expected" ]]; then
    echo "expected '$expected', got '$actual' from: $*" >&2
    exit 1
  fi
}

assert_failure() {
  if "$SCRIPT" "$@" >/dev/null 2>&1; then
    echo "expected failure from: $*" >&2
    exit 1
  fi
}

assert_output minor bump 'feat: add release automation'
assert_output minor bump 'feat(release): add version tags'
assert_output major bump 'feat!: replace public contract'
assert_output major bump 'fix(api)!: remove old response'
assert_output patch bump 'fix: correct image label'
assert_output patch bump 'docs(release): explain recovery'

assert_output 0.1.0 next '' 'feat: bootstrap releases'
assert_output 0.2.0 next v0.1.0 'feat: add release automation'
assert_output 0.1.1 next v0.1.0 'fix: correct image label'
assert_output 1.0.0 next v0.8.4 'feat!: replace public contract'
assert_output 0.10.0 next v0.9.9 'feat: add capability'

assert_output 003-release-versioning validate-branch 003-release-versioning
assert_failure validate-branch main
assert_failure validate-branch feature/release
assert_failure bump 'Feature: mixed case is invalid'
assert_failure bump 'feat: '
assert_failure bump 'unknown: change'
assert_failure next v1.2 'fix: malformed previous tag'

echo "release version policy tests passed"
