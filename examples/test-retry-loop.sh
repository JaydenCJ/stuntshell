#!/usr/bin/env bash
# Demonstrates scripted takes: the curl double fails twice with a connection
# error, then reports healthy — so you can test an agent's retry/backoff
# logic without a server, a network, or a sleep.
#
# Usage: bash examples/test-retry-loop.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

go build -o "$WORKDIR/stuntshell" "$ROOT/cmd/stuntshell"
"$WORKDIR/stuntshell" build --manifest "$ROOT/examples/retry-loop.json" --out "$WORKDIR/.stunts" >/dev/null
export PATH="$WORKDIR/.stunts/bin:$PATH"

# The retry loop under test: poll the health endpoint until it succeeds.
attempts=0
until curl -fsS http://127.0.0.1:8080/health; do
  attempts=$((attempts + 1))
  [ "$attempts" -lt 10 ] || { echo "gave up" >&2; exit 1; }
done

# Judge: healthy within 3–5 attempts, per the manifest's expectation.
"$WORKDIR/stuntshell" verify \
  --manifest "$ROOT/examples/retry-loop.json" \
  --log "$WORKDIR/.stunts/invocations.jsonl"
