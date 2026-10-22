#!/bin/sh
# Controlled historical source import. No router or VPS operations.
# Default is --plan only; --apply requires explicit approval and git-subtree.
set -eu

SOURCE=https://github.com/saymer-alt/wdtt-suite.git
SOURCE_REF=refs/heads/release/wdtt-pr34-sec41-owner-20261022
SOURCE_SHA=d39f9dad91639a0f1ca321df72f3dc51ff0b0af0
PREFIX=transports/wdtt

usage() {
    printf '%s\n' 'Usage: scripts/import-wdtt-history.sh --plan | --apply'
    printf '%s\n' '  --plan   Print immutable source, expected SHA, and migration instructions (no network or changes)'
    printf '%s\n' '  --apply  Import pinned WDTT source with full ancestry to the CURRENT draft migration branch'
}

case "${1:---plan}" in
--plan)
    printf 'Source: %s\nBranch: %s\nPinned SHA: %s\nDestination: %s\n' "$SOURCE" "$SOURCE_REF" "$SOURCE_SHA" "$PREFIX"
    printf '%s\n' 'Requirements: new KeenWhite repo created, git subtree installed, clean branch named migration/wdtt-*, owner-approved import, provenance/secret checks.'
    printf '%s\n' 'This dry run does not access the network.'
    exit 0 ;;
--apply) : ;;
*) usage >&2; exit 2 ;;
esac

if [ "${KEENWHITE_APPROVE_WDTT_IMPORT:-}" != "$SOURCE_SHA" ]; then
    printf '%s\n' 'BLOCKED: set KEENWHITE_APPROVE_WDTT_IMPORT to the exact pinned full SHA before --apply.' >&2
    exit 1
fi

root=$(git rev-parse --show-toplevel 2>/dev/null) || { echo 'BLOCKED: not inside Git repository' >&2; exit 1; }
cd "$root"
branch=$(git symbolic-ref --short HEAD 2>/dev/null) || { echo 'BLOCKED: detached HEAD' >&2; exit 1; }
case "$branch" in
migration/wdtt-*) : ;;
*) printf 'BLOCKED: branch %s must be migration/wdtt-* (never main).\n' "$branch" >&2; exit 1 ;;
esac
if [ -n "$(git status --porcelain)" ]; then
    echo 'BLOCKED: working tree is not clean.' >&2
    exit 1
fi
if [ -e "$PREFIX" ]; then
    printf 'BLOCKED: %s already exists.\n' "$PREFIX" >&2
    exit 1
fi
if [ ! -f "$(git --exec-path)/git-subtree" ] && [ ! -x "$(git --exec-path)/git-subtree" ]; then
    echo 'BLOCKED: git subtree is unavailable; install git-subtree before proceeding.' >&2
    exit 1
fi
if git remote get-url keenwhite-wdtt >/dev/null 2>&1; then
    actual=$(git remote get-url keenwhite-wdtt)
    if [ "$actual" != "$SOURCE" ]; then
        printf 'BLOCKED: existing remote keenwhite-wdtt is %s, not expected URL.\n' "$actual" >&2
        exit 1
    fi
else
    git remote add keenwhite-wdtt "$SOURCE"
fi
# Only this source branch is fetched; tags/releases are not moved or replaced.
git fetch --no-tags keenwhite-wdtt "+$SOURCE_REF:refs/remotes/keenwhite-wdtt/wdtt-approved"
actual=$(git rev-parse refs/remotes/keenwhite-wdtt/wdtt-approved)
if [ "$actual" != "$SOURCE_SHA" ]; then
    printf 'BLOCKED: WDTT source branch moved; expected %s got %s.\n' "$SOURCE_SHA" "$actual" >&2
    exit 1
fi
printf '%s\n' 'WARNING: inspect the full upstream Git history for credentials and notices BEFORE import.'
if [ "${KEENWHITE_HISTORY_AUDITED:-}" != yes ]; then
    echo 'BLOCKED: set KEENWHITE_HISTORY_AUDITED=yes only after auditing the history to be imported.' >&2
    exit 1
fi
# No --squash! Preserve Git ancestry and upstream attribution.
git subtree add --prefix="$PREFIX" "$SOURCE_SHA" -m "import(wdtt): preserve GPL-3.0 source history at $SOURCE_SHA"
printf '%s\n' 'WDTT subtree imported into migration branch. Open a Draft PR; do not merge without license/security/CI review.'
