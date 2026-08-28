#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: release-version.sh {bump <title>|next <latest-tag-or-empty> <title>|validate-branch <branch>}" >&2
  exit 2
}

validate_title() {
  local title=$1
  local pattern='^(feat|fix|perf|refactor|docs|test|build|ci|chore|revert)(\([a-z0-9-]+\))?(!)?: ([^[:space:]].*)$'
  if [[ ! $title =~ $pattern ]]; then
    echo "invalid pull-request title; expected conventional form such as 'feat(scope): describe change'" >&2
    return 1
  fi
}

bump_for_title() {
  local title=$1
  validate_title "$title"
  if [[ ${BASH_REMATCH[3]} == '!' ]]; then
    echo major
  elif [[ ${BASH_REMATCH[1]} == 'feat' ]]; then
    echo minor
  else
    echo patch
  fi
}

next_version() {
  local latest=$1
  local title=$2
  validate_title "$title"

  if [[ -z $latest ]]; then
    echo 0.1.0
    return
  fi

  local version_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
  if [[ ! $latest =~ $version_pattern ]]; then
    echo "invalid latest release tag '$latest'; expected vMAJOR.MINOR.PATCH" >&2
    return 1
  fi

  local major=${BASH_REMATCH[1]}
  local minor=${BASH_REMATCH[2]}
  local patch=${BASH_REMATCH[3]}
  local bump
  bump=$(bump_for_title "$title")

  case $bump in
    major) ((major += 1)); minor=0; patch=0 ;;
    minor) ((minor += 1)); patch=0 ;;
    patch) ((patch += 1)) ;;
    *) echo "unsupported bump '$bump'" >&2; return 1 ;;
  esac
  printf '%d.%d.%d\n' "$major" "$minor" "$patch"
}

validate_branch() {
  local branch=$1
  if [[ ! $branch =~ ^[0-9]{3}-[a-z0-9]+(-[a-z0-9]+)*$ ]]; then
    echo "invalid feature branch '$branch'; expected NNN-lowercase-kebab" >&2
    return 1
  fi
  echo "$branch"
}

case ${1:-} in
  bump)
    [[ $# -eq 2 ]] || usage
    bump_for_title "$2"
    ;;
  next)
    [[ $# -eq 3 ]] || usage
    next_version "$2" "$3"
    ;;
  validate-branch)
    [[ $# -eq 2 ]] || usage
    validate_branch "$2"
    ;;
  *) usage ;;
esac
