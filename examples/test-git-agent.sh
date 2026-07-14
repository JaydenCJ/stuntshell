#!/usr/bin/env bash
# A complete agent test in one file: stage doubles from examples/git-agent.json,
# run a stand-in "agent" that commits and pushes, then verify the invocation
# journal against the manifest's expectations. Offline, deterministic.
#
# Usage: bash examples/test-git-agent.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

# 1. Build stuntshell and stage the doubles.
go build -o "$WORKDIR/stuntshell" "$ROOT/cmd/stuntshell"
"$WORKDIR/stuntshell" build --manifest "$ROOT/examples/git-agent.json" --out "$WORKDIR/.stunts" >/dev/null
export PATH="$WORKDIR/.stunts/bin:$PATH"

# 2. The "agent under test". In a real suite this would be your AI agent
#    with its shell tool pointed at the doubled PATH.
agent() {
  git status --porcelain
  git add src/app.js
  git commit -m "Fix login redirect"
  git push origin main
}
agent

# 3. Judge the journal. --strict also fails on any invocation no rule
#    anticipated (the manifest sets strict + ordered already).
"$WORKDIR/stuntshell" verify \
  --manifest "$ROOT/examples/git-agent.json" \
  --log "$WORKDIR/.stunts/invocations.jsonl"
