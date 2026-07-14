# Contributing to stuntshell

Issues, discussions and pull requests are all welcome.

## Getting started

You need Go ≥1.22 and a POSIX shell; nothing else.

```bash
git clone https://github.com/JaydenCJ/stuntshell && cd stuntshell
go build ./...
go test ./...
bash scripts/smoke.sh
```

`scripts/smoke.sh` builds the binary, generates doubles from the starter
manifest, runs them through PATH exactly as an agent harness would, and
asserts on real output and exit codes; it must finish by printing `SMOKE OK`.

## Before you open a pull request

1. `gofmt -l .` reports nothing (formatting is enforced).
2. `go vet ./...` passes with no findings.
3. `go test ./...` passes (90 deterministic tests, no network).
4. `bash scripts/smoke.sh` prints `SMOKE OK`.
5. Add tests for behavior changes; keep logic in pure, unit-testable
   modules (matching, expansion, and verification never touch the
   filesystem — only `journal` and `shim` do).

## Ground rules

- Keep dependencies at zero — stuntshell is Go standard library only, and
  the generated shims are plain POSIX sh. Adding a dependency needs strong
  justification in the PR.
- No network calls, ever, in the tool or in its tests; examples use
  127.0.0.1 and never connect to it. No telemetry.
- Determinism first: identical invocations must produce byte-identical
  journals and reports. Nothing time-based goes into a journal record.
- Manifest semantics are contract: any change to matching, sequencing, or
  expectation evaluation needs a docs/manifest.md update and tests for
  both the pass and the fail direction.
- Code comments and doc comments are written in English.

## Reporting bugs

Include the output of `stuntshell version`, the manifest (redacted if
needed), the journal (`stuntshell log --format json`), the full command you
ran, and what you expected — the journal is exactly what `verify` saw, so
with those four things almost every report is reproducible.

## Security

Please do not open public issues for security problems; use GitHub's
private vulnerability reporting on this repository instead.
