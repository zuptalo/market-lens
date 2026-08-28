#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
CI="$ROOT/.github/workflows/ci.yml"
RELEASE="$ROOT/.github/workflows/release.yml"

require_text() {
  local file=$1
  local text=$2
  if ! grep -Fq -- "$text" "$file"; then
    echo "expected '$text' in ${file#$ROOT/}" >&2
    exit 1
  fi
}

require_order() {
  local file=$1
  local first=$2
  local second=$3
  local first_line second_line
  first_line=$(grep -Fn -- "$first" "$file" | head -1 | cut -d: -f1)
  second_line=$(grep -Fn -- "$second" "$file" | head -1 | cut -d: -f1)
  if [[ -z $first_line || -z $second_line || $first_line -ge $second_line ]]; then
    echo "expected '$first' before '$second' in ${file#$ROOT/}" >&2
    exit 1
  fi
}

require_text "$CI" 'name: PR policy'
require_text "$CI" 'name: Frontend'
require_text "$CI" 'name: Backend'
require_text "$CI" 'name: End-to-end'
require_text "$CI" 'name: Container validation'
require_text "$CI" 'name: Required checks'
require_text "$CI" 'needs: [policy, frontend, backend, e2e, container]'
require_text "$CI" 'scripts/release-version.sh validate-branch "$PR_BRANCH"'
require_text "$CI" 'scripts/release-version.sh bump "$PR_TITLE"'
require_text "$CI" 'PR_TITLE: ${{ github.event.pull_request.title }}'
require_text "$CI" 'if: ${{ always() }}'

if [[ ! -f $RELEASE ]]; then
  echo "expected release workflow at ${RELEASE#$ROOT/}" >&2
  exit 1
fi

require_text "$RELEASE" 'branches: [main]'
require_text "$RELEASE" 'group: market-lens-release-main'
require_text "$RELEASE" 'cancel-in-progress: false'
require_text "$RELEASE" 'queue: max'
require_text "$RELEASE" 'contents: write'
require_text "$RELEASE" 'packages: write'
require_text "$RELEASE" 'attestations: write'
require_text "$RELEASE" 'id-token: write'
require_text "$RELEASE" 'scripts/release-version.test.sh'
require_text "$RELEASE" 'scripts/release-version.sh next'
require_text "$RELEASE" 'git tag --points-at "$GITHUB_SHA"'
require_text "$RELEASE" 'published=true'
require_text "$RELEASE" "if: steps.release_state.outputs.published != 'true'"
require_text "$RELEASE" 'type=raw,value=${{ steps.version.outputs.version }}'
require_text "$RELEASE" 'type=raw,value=${{ steps.version.outputs.major_minor }}'
require_text "$RELEASE" 'type=raw,value=${{ steps.version.outputs.major }}'
require_text "$RELEASE" 'type=sha,prefix=sha-,format=long'
require_text "$RELEASE" 'type=raw,value=latest'
require_text "$RELEASE" 'VERSION=${{ steps.version.outputs.version }}'
require_text "$RELEASE" 'provenance: mode=max'
require_text "$RELEASE" 'sbom: true'
require_text "$RELEASE" '--draft --generate-notes'
require_text "$RELEASE" '--draft=false --latest'
require_order "$RELEASE" 'push: true' '--draft=false --latest'

echo "workflow contract tests passed"
