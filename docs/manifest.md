# The stuntshell manifest format

One JSON document (conventionally `stuntshell.json`) describes the whole
stage: which doubles exist, how each one answers, and what the invocation
journal must look like afterwards. Parsing is strict — unknown keys are
rejected, so a typo like `"stdotu"` fails at `build` time instead of
silently producing a do-nothing double.

```json
{
  "version": 1,
  "strict": false,
  "ordered": false,
  "commands": { "<name>": { … } },
  "expect": [ { … } ]
}
```

| Key | Required | Effect |
|---|---|---|
| `version` | yes | manifest schema version; this release understands `1` |
| `strict` | no | `verify` fails if any invocation hit no rule (same as `--strict`) |
| `ordered` | no | first matches of required expectations must appear in listed order |
| `commands` | yes | one entry per double; the key becomes the executable name |
| `expect` | no | invocation assertions evaluated by `verify` |

Command names become files in the generated `bin/` directory, so they must
be plain file names: no path separators, whitespace, or control characters,
and not `stuntshell` itself.

## Commands

```json
"git": {
  "capture_stdin": false,
  "default": { "exit": 1, "stderr": "git: unexpected: {argv}\n" },
  "rules": [ … ]
}
```

| Key | Default | Effect |
|---|---|---|
| `capture_stdin` | `false` | record up to 64 KiB of the double's stdin in the journal |
| `default` | built-in | response when no rule matches (built-in: exit 1 + diagnostic on stderr) |
| `rules` | `[]` | tried top to bottom; the first match wins |

A default answer is journaled with `"matched": false` and `"rule": -1`, so
strict verification can reject invocations nobody anticipated.

## Rules and the pattern language

```json
{ "match": ["push", "origin", "*"], "exit": 0 }
```

`match` is a list of per-token patterns compared positionally against argv
(everything after the command name):

| Pattern form | Matches |
|---|---|
| `status` | exactly that token (case-sensitive) |
| `*` | any run of characters, including none |
| `v?` | `?` matches exactly one character (one rune) |
| `\*`, `\?` | the literal character after the backslash |
| `"..."` (final position only) | the remainder of argv, zero or more tokens |
| `"match"` omitted | ANY argv — a catch-all rule |
| `"match": []` | only an empty argv |

Without a trailing `"..."`, argv must have exactly as many tokens as the
pattern list — a rule for `["push", "origin"]` does not match
`push origin main`.

## Responses, placeholders, and scripts

A rule carries either one inline response (`stdout`, `stderr`, `exit` —
all optional, exit defaults to 0) or a `script` array of responses:

```json
{ "match": ["fetch", "..."], "script": [
  { "exit": 128, "stderr": "fatal: unable to access remote\n" },
  { "exit": 0 }
] }
```

The nth invocation matching that rule gets the nth take; past the end, the
last take repeats. The counter lives in the journal, so sequencing works
across processes (invocations of the same command are assumed sequential,
the normal shape of an agent test) and `reset` rewinds it.

`stdout` and `stderr` may use placeholders, expanded per invocation:

| Placeholder | Expands to |
|---|---|
| `{cmd}` | the command name |
| `{argv}` | all arguments joined with single spaces |
| `{0}`, `{1}`, … | the argument at that index (empty when out of range) |
| `{#}` | the number of arguments |
| `{nth}` | 1-based per-rule invocation ordinal |
| `{{`, `}}` | literal braces |

Unknown placeholders are left verbatim so manifest typos stay visible.

## Expectations

```json
{ "command": "git", "args_glob": ["push", "--force", "..."], "max": 0,
  "description": "never force-push" }
```

| Key | Default | Effect |
|---|---|---|
| `command` | required | must name a declared double (typos fail at load) |
| `args` | any argv | exact argv, token for token (`[]` = bare invocation) |
| `args_glob` | — | argv pattern using the rule language above |
| `min` / `max` | min 1 | inclusive count bounds; `"max": 0` means "never" |
| `exactly` | — | pins the count; excludes `min` and `max` |
| `description` | — | label used in `verify` reports |

With no count field at all, an expectation means "called at least once".
With `"ordered": true` at the top level, the first matching invocation of
each required expectation (effective min ≥ 1) must appear in the journal in
the order the expectations are listed.

## The journal

Every invocation appends one JSON line — no timestamps, so identical runs
produce byte-identical journals:

```json
{"command":"git","args":["push","origin","main"],"rule":3,"matched":true,"exit":0,"dir":"/work/repo"}
```

`stuntshell log` renders it for humans, `stuntshell log --format json` for
machines, and `stuntshell verify` / `stuntshell assert` judge it.
