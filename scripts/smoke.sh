#!/usr/bin/env bash
# End-to-end smoke test for stuntshell: builds the binary, generates doubles
# from the starter manifest, runs them through PATH exactly as an agent
# harness would, and asserts on real output and exit codes. No network,
# idempotent, finishes in seconds.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

fail() {
  echo "SMOKE FAIL: $*" >&2
  exit 1
}

# has HAYSTACK NEEDLE — substring check without a pipeline. Piping a command
# straight into `grep -q` is flaky under pipefail: grep exits at the first
# match, and the writer dies of SIGPIPE if it is still writing.
has() {
  case "$1" in *"$2"*) return 0 ;; *) return 1 ;; esac
}

BIN="$WORKDIR/stuntshell"
MANIFEST="$WORKDIR/stuntshell.json"
OUT="$WORKDIR/.stunts"
LOG="$OUT/invocations.jsonl"

echo "1. build"
(cd "$ROOT" && go build -o "$BIN" ./cmd/stuntshell) || fail "go build failed"

echo "2. version matches manifest"
[ "$("$BIN" --version)" = "stuntshell 0.1.0" ] || fail "--version mismatch"

echo "3. init writes a valid starter manifest"
"$BIN" init "$MANIFEST" >/dev/null || fail "init failed"
grep -q '"version": 1' "$MANIFEST" || fail "starter manifest malformed"

echo "4. build stages the doubles and prints the export line"
BUILD_OUT="$("$BIN" build --manifest "$MANIFEST" --out "$OUT")"
has "$BUILD_OUT" "2 doubles staged" || fail "double inventory missing"
has "$BUILD_OUT" "export PATH=" || fail "export line missing"
[ -x "$OUT/bin/git" ] || fail "git double not executable"
[ -x "$OUT/bin/kubectl" ] || fail "kubectl double not executable"

echo "5. doubles answer through PATH like real commands"
export PATH="$OUT/bin:$PATH"
has "$(command -v git)" "$OUT/bin/git" || fail "PATH does not resolve to the double"
has "$(git status)" "On branch main" || fail "git status double wrong"
git push origin main || fail "git push double should exit 0"
has "$(kubectl get pods --namespace prod)" "api-0" || fail "kubectl double wrong"

echo "6. scripted takes: fetch fails once, then recovers"
if git fetch origin 2>/dev/null; then
  fail "first fetch should fail with exit 128"
fi
git fetch origin || fail "second fetch should succeed"

echo "7. unmatched argv hits the default response"
set +e
UNMATCHED_ERR="$(git frobnicate 2>&1)"
UNMATCHED_CODE=$?
set -e
[ "$UNMATCHED_CODE" -eq 1 ] || fail "default response should exit 1"
has "$UNMATCHED_ERR" "no stunt rule matched: frobnicate" || fail "default stderr wrong"

echo "8. the journal recorded everything in order"
LOG_OUT="$("$BIN" log --log "$LOG")"
has "$LOG_OUT" "invocations" || fail "log header missing"
has "$LOG_OUT" "git push origin main" || fail "push not journaled"
has "$("$BIN" log --log "$LOG" --command kubectl)" "1 invocation" || fail "log filter wrong"

echo "9. verify passes the manifest's expectations"
has "$("$BIN" verify --manifest "$MANIFEST" --log "$LOG")" "verify: PASS" || fail "verify should pass"

echo "10. a forbidden force-push flips verify to FAIL with exit 1"
git push --force origin main 2>/dev/null || true
if "$BIN" verify --manifest "$MANIFEST" --log "$LOG" >/dev/null; then
  fail "verify should fail after a force-push"
fi
VERIFY_OUT="$("$BIN" verify --manifest "$MANIFEST" --log "$LOG" || true)"
has "$VERIFY_OUT" "never force-push" || fail "verify should name the failed expectation"

echo "11. ad-hoc assert agrees in both directions"
"$BIN" assert --log "$LOG" --command git --args "push origin main" --min 1 >/dev/null \
  || fail "assert min 1 should pass"
if "$BIN" assert --log "$LOG" --command git --args-glob "push --force ..." --max 0 >/dev/null; then
  fail "assert max 0 should fail after the force-push"
fi

echo "12. reset starts a fresh test case"
"$BIN" reset --log "$LOG" >/dev/null || fail "reset failed"
has "$("$BIN" log --log "$LOG")" "0 invocations" || fail "journal should be empty"

echo "13. usage errors exit 2"
set +e
"$BIN" log --log "$LOG" --format yaml >/dev/null 2>&1
[ $? -eq 2 ] || fail "bad --format should exit 2"
set -e

echo "SMOKE OK"
