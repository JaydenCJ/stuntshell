# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-13

### Added

- Declarative manifest (`stuntshell.json`, strict JSON with unknown-field
  rejection) describing stunt doubles: per-command rule lists matched
  first-wins against argv, with per-token globs (`*`, `?`, backslash
  escapes) and a `"..."` rest token; omitted `match` as catch-all.
- Responses with `stdout`/`stderr`/`exit` and placeholder expansion
  (`{cmd}`, `{argv}`, `{0}`…, `{#}`, `{nth}`, `{{`/`}}` escapes).
- Scripted takes: a rule's `script` array answers the nth match with its
  nth response (last one repeats), for fail-twice-then-succeed retry tests
  — sequenced through the journal, so it works across processes.
- `build` subcommand generating one self-contained POSIX sh shim per
  command (absolute paths baked in, single-quote-safe), plus `init`,
  `path`, and a `--bin` override; rebuilds never truncate the journal.
- Timestamp-free JSONL invocation journal (argv, matched rule, exit,
  working directory, optional 64 KiB-capped stdin capture) written with
  single O_APPEND writes; `log` lists it in text or JSON, `reset` starts a
  fresh test case.
- Invocation assertions: manifest `expect` entries with exact or glob argv,
  `min`/`max`/`exactly` counts, list-order enforcement via `ordered`, and
  a strict mode failing any invocation no rule anticipated; evaluated by
  `verify` (text/JSON report, exit 1 on failure) and ad-hoc by `assert`.
- Default responses per command plus a built-in diagnostic fallback, so an
  unanticipated invocation is always visible and never a crash.
- Runnable examples (`examples/test-git-agent.sh`,
  `examples/test-retry-loop.sh`) and a manifest format reference
  (`docs/manifest.md`).
- 90 deterministic offline tests (unit + in-process CLI integration) and
  `scripts/smoke.sh` exercising the shims end-to-end through PATH.

[0.1.0]: https://github.com/JaydenCJ/stuntshell/releases/tag/v0.1.0
