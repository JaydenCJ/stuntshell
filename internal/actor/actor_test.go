// Unit tests for the double runtime: rule precedence, script sequencing
// across processes (via the journal), placeholder expansion, and the
// default/fallback ladder. Every test uses a temp journal — no shared
// state, no ordering requirements between tests.
package actor

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaydenCJ/stuntshell/internal/journal"
	"github.com/JaydenCJ/stuntshell/internal/manifest"
)

// load parses a manifest snippet, failing the test on error.
func load(t *testing.T, src string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return m
}

func tempLog(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "invocations.jsonl")
}

// act runs one invocation, failing the test on runtime errors.
func act(t *testing.T, m *manifest.Manifest, log, cmd string, args ...string) Result {
	t.Helper()
	res, err := Act(m, log, Invocation{Command: cmd, Args: args})
	if err != nil {
		t.Fatalf("Act: %v", err)
	}
	return res
}

const gitManifest = `{
  "version": 1,
  "commands": {
    "git": {
      "default": {"exit": 1, "stderr": "git: nope: {argv}\n"},
      "rules": [
        {"match": ["status"], "stdout": "clean\n"},
        {"match": ["push", "..."], "stdout": "pushed {argv}\n"},
        {"match": ["..."], "stdout": "generic\n", "exit": 3}
      ]
    }
  }
}`

func TestFirstMatchingRuleWins(t *testing.T) {
	m := load(t, gitManifest)
	res := act(t, m, tempLog(t), "git", "status")
	if res.Stdout != "clean\n" || res.Rule != 0 || !res.Matched {
		t.Fatalf("rule 0 should win: %+v", res)
	}
}

func TestLaterRuleReachedWhenEarlierMisses(t *testing.T) {
	m := load(t, gitManifest)
	res := act(t, m, tempLog(t), "git", "log", "--oneline")
	if res.Rule != 2 || res.Exit != 3 || res.Stdout != "generic\n" {
		t.Fatalf("catch-all rule should answer: %+v", res)
	}
}

func TestPlaceholdersExpandAgainstInvocation(t *testing.T) {
	m := load(t, gitManifest)
	res := act(t, m, tempLog(t), "git", "push", "origin", "main")
	if res.Stdout != "pushed push origin main\n" {
		t.Fatalf("got %q", res.Stdout)
	}
}

func TestDefaultResponseWhenNoRuleMatches(t *testing.T) {
	m := load(t, `{"version": 1, "commands": {"git": {
		"default": {"exit": 4, "stderr": "custom: {argv}\n"},
		"rules": [{"match": ["status"]}]}}}`)
	res := act(t, m, tempLog(t), "git", "frobnicate", "-x")
	if res.Exit != 4 || res.Stderr != "custom: frobnicate -x\n" {
		t.Fatalf("default should answer with templates expanded: %+v", res)
	}
	if res.Matched || res.Rule != -1 {
		t.Fatal("a default answer must be recorded as unmatched, rule -1")
	}
}

func TestBuiltInFallbackWithoutDefault(t *testing.T) {
	m := load(t, `{"version": 1, "commands": {"git": {"rules": [{"match": ["status"]}]}}}`)
	res := act(t, m, tempLog(t), "git", "frobnicate")
	if res.Exit != 1 {
		t.Fatalf("built-in fallback should exit 1, got %d", res.Exit)
	}
	if !strings.Contains(res.Stderr, "no rule matched: frobnicate") {
		t.Fatalf("fallback should name the argv: %q", res.Stderr)
	}
	// An argument that LOOKS like a placeholder must come out verbatim —
	// expansion is single-pass by design.
	res = act(t, m, tempLog(t), "git", "{cmd}")
	if !strings.Contains(res.Stderr, "no rule matched: {cmd}") {
		t.Fatalf("argv must not be re-expanded: %q", res.Stderr)
	}
}

func TestScriptStepsThroughTakesThenRepeatsLast(t *testing.T) {
	m := load(t, `{"version": 1, "commands": {"git": {"rules": [
		{"match": ["fetch"], "script": [
			{"exit": 128, "stderr": "fatal: unable to access remote\n"},
			{"exit": 128, "stderr": "fatal: unable to access remote\n"},
			{"exit": 0, "stdout": "ok on attempt {nth}\n"}
		]}]}}}`)
	log := tempLog(t)
	first := act(t, m, log, "git", "fetch")
	second := act(t, m, log, "git", "fetch")
	third := act(t, m, log, "git", "fetch")
	fourth := act(t, m, log, "git", "fetch")
	if first.Exit != 128 || second.Exit != 128 {
		t.Fatal("first two takes should fail")
	}
	if third.Exit != 0 || third.Stdout != "ok on attempt 3\n" {
		t.Fatalf("third take should succeed with {nth}=3: %+v", third)
	}
	if fourth.Exit != 0 {
		t.Fatal("past the end, the last take repeats")
	}
}

func TestScriptCountersArePerRule(t *testing.T) {
	// Interleaved invocations of other rules must not advance a script.
	m := load(t, `{"version": 1, "commands": {"git": {"rules": [
		{"match": ["status"], "stdout": "clean\n"},
		{"match": ["fetch"], "script": [{"exit": 1}, {"exit": 0}]}]}}}`)
	log := tempLog(t)
	act(t, m, log, "git", "fetch")
	act(t, m, log, "git", "status")
	act(t, m, log, "git", "status")
	res := act(t, m, log, "git", "fetch")
	if res.Exit != 0 {
		t.Fatalf("second fetch should hit the second take, got exit %d", res.Exit)
	}
}

func TestSequencingSurvivesProcessBoundaries(t *testing.T) {
	// Each Act call reads the journal fresh — exactly what happens when
	// every invocation is a separate shim process.
	src := `{"version": 1, "commands": {"git": {"rules": [
		{"match": ["fetch"], "script": [{"exit": 1}, {"exit": 0}]}]}}}`
	log := tempLog(t)
	first := act(t, load(t, src), log, "git", "fetch")
	second := act(t, load(t, src), log, "git", "fetch") // fresh manifest = fresh process
	if first.Exit != 1 || second.Exit != 0 {
		t.Fatalf("sequencing must persist in the journal: %d then %d", first.Exit, second.Exit)
	}
}

func TestEveryInvocationIsJournaled(t *testing.T) {
	m := load(t, gitManifest)
	log := tempLog(t)
	act(t, m, log, "git", "status")
	act(t, m, log, "git", "frobnicate", "--hard")
	records, err := journal.Read(log)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("want 2 records, got %d", len(records))
	}
	if !records[0].Matched || records[0].Rule != 0 {
		t.Fatalf("matched invocation misrecorded: %+v", records[0])
	}
	// "frobnicate --hard" matches the catch-all rule 2 in gitManifest.
	if records[1].Rule != 2 || records[1].Args[1] != "--hard" {
		t.Fatalf("argv misrecorded: %+v", records[1])
	}
}

func TestStdinLandsInTheRecord(t *testing.T) {
	m := load(t, `{"version": 1, "commands": {"kubectl": {"capture_stdin": true,
		"rules": [{"match": ["apply", "-f", "-"], "stdout": "applied\n"}]}}}`)
	log := tempLog(t)
	_, err := Act(m, log, Invocation{Command: "kubectl", Args: []string{"apply", "-f", "-"},
		Stdin: "kind: Pod\n", StdinTruncated: false})
	if err != nil {
		t.Fatal(err)
	}
	records, err := journal.Read(log)
	if err != nil {
		t.Fatal(err)
	}
	if records[0].Stdin != "kind: Pod\n" {
		t.Fatalf("stdin should be journaled: %+v", records[0])
	}
}

func TestUnknownCommandIsARuntimeError(t *testing.T) {
	// A shim for a command that was removed from the manifest must fail
	// loudly, not fake an answer.
	m := load(t, gitManifest)
	if _, err := Act(m, tempLog(t), Invocation{Command: "kubectl"}); err == nil {
		t.Fatal("acting for an undeclared double must error")
	}
}
