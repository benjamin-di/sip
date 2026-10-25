#!/usr/bin/env bash
# Succeeds when $WORKFLOW has a successful run for $SHA; waits (poll ${POLL:-20}s,
# up to $WAIT s) while one is queued or running; fails on failure or no run.
set -euo pipefail
deadline=$((SECONDS + ${WAIT:-1200}))
while :; do
    runs="$(gh run list -R "$REPO" --workflow "$WORKFLOW" --commit "$SHA" --limit 20 \
        --json status,conclusion,url --jq '.[] | "\(.status) \(.conclusion) \(.url)"')"
    if grep -q '^completed success ' <<<"$runs"; then
        echo "$WORKFLOW passed for ${SHA:0:12}: $(grep '^completed success ' <<<"$runs" | head -1 | awk '{print $3}')"
        exit 0
    fi
    if ! grep -qvE '^completed ' <<<"$runs"; then
        if [ -n "$runs" ]; then
            echo "::error::$WORKFLOW did not succeed for ${SHA:0:12}: $(head -1 <<<"$runs")"
        else
            echo "::error::no $WORKFLOW run for ${SHA:0:12}; push the commit to the default branch first (tag only CI-verified commits)"
        fi
        exit 1
    fi
    if [ "$SECONDS" -ge "$deadline" ]; then
        echo "::error::$WORKFLOW still running for ${SHA:0:12} after ${WAIT}s"
        exit 1
    fi
    sleep "${POLL:-20}"
done
