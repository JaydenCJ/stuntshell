// Integration tests: drive the full CLI in-process (Run with captured
// streams) through realistic init → build → act → log/verify/assert/reset
// flows in temp directories. Everything is offline and deterministic; the
// only thing not covered here is the sh shim exec itself, which
// scripts/smoke.sh exercises against the compiled binary.
package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaydenCJ/stuntshell/internal/verify"
)

// run invokes the CLI in-process and captures both streams.
func run(t *testing.T, stdin io.Reader, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf strings.Builder
	code = Run(args, stdin, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

// stage writes the starter manifest into a temp dir and builds its doubles
// there, returning the manifest and journal paths.
func stage(t *testing.T) (manifestPath, logPath string) {
	t.Helper()
	dir := t.TempDir()
	manifestPath = filepath.Join(dir, "stuntshell.json")
	code, _, stderr := run(t, nil, "init", manifestPath)
	if code != ExitOK {
		t.Fatalf("init failed (%d): %s", code, stderr)
	}
	out := filepath.Join(dir, ".stunts")
	code, _, stderr = run(t, nil, "build", "--manifest", manifestPath, "--out", out, "--bin", "/opt/stuntshell")
	if code != ExitOK {
		t.Fatalf("build failed (%d): %s", code, stderr)
	}
	return manifestPath, filepath.Join(out, "invocations.jsonl")
}

// act invokes a double in-process, as the shim's exec would.
func act(t *testing.T, manifestPath, logPath string, argv ...string) (int, string, string) {
	t.Helper()
	args := append([]string{"__act", "--manifest", manifestPath, "--log", logPath, "--"}, argv...)
	return run(t, strings.NewReader(""), args...)
}

func TestVersionPrintsNameAndSemver(t *testing.T) {
	code, stdout, _ := run(t, nil, "version")
	if code != ExitOK || stdout != "stuntshell 0.1.0\n" {
		t.Fatalf("got code %d, output %q", code, stdout)
	}
}

func TestHelpListsEverySubcommand(t *testing.T) {
	code, stdout, _ := run(t, nil, "--help")
	if code != ExitOK {
		t.Fatalf("help exit %d", code)
	}
	for _, sub := range []string{"init", "build", "path", "log", "verify", "assert", "reset"} {
		if !strings.Contains(stdout, "stuntshell "+sub) {
			t.Fatalf("help is missing %q", sub)
		}
	}
	code, _, stderr := run(t, nil, "frobnicate")
	if code != ExitUsage || !strings.Contains(stderr, "unknown command") {
		t.Fatalf("unknown subcommand: got code %d, stderr %q", code, stderr)
	}
}

func TestInitManifestIsValidAndBuildable(t *testing.T) {
	// The starter manifest is the first thing every user runs; it must
	// always load, validate, and build (stage does init + build).
	manifestPath, logPath := stage(t)
	if _, err := os.Stat(logPath); err != nil {
		t.Fatal("build should create the journal")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil || !strings.Contains(string(data), `"version": 1`) {
		t.Fatalf("starter manifest malformed: %v", err)
	}
	// Re-running init must refuse to clobber the edited cast…
	code, _, stderr := run(t, nil, "init", manifestPath)
	if code != ExitRuntime || !strings.Contains(stderr, "already exists") {
		t.Fatalf("second init should refuse: %d %q", code, stderr)
	}
	// …unless the user explicitly forces it.
	if code, _, _ := run(t, nil, "init", "--force", manifestPath); code != ExitOK {
		t.Fatal("--force should overwrite")
	}
}

func TestBuildPrintsInventoryAndExportLine(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "stuntshell.json")
	run(t, nil, "init", manifestPath)
	out := filepath.Join(dir, ".stunts")
	code, stdout, _ := run(t, nil, "build", "--manifest", manifestPath, "--out", out, "--bin", "/opt/stuntshell")
	if code != ExitOK {
		t.Fatal("build should succeed")
	}
	if !strings.Contains(stdout, "2 doubles staged") || !strings.Contains(stdout, "git, kubectl") {
		t.Fatalf("inventory missing: %q", stdout)
	}
	if !strings.Contains(stdout, `export PATH='`+filepath.Join(out, "bin")+`':"$PATH"`) {
		t.Fatalf("export line missing: %q", stdout)
	}
}

func TestBuildFailsCleanlyOnBrokenManifest(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"version": 1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := run(t, nil, "build", "--manifest", bad, "--out", filepath.Join(dir, ".stunts"))
	if code != ExitRuntime || !strings.Contains(stderr, "at least one command") {
		t.Fatalf("got code %d, stderr %q", code, stderr)
	}
}

func TestPathPrintsExportLineAfterBuild(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "stuntshell.json")
	run(t, nil, "init", manifestPath)
	out := filepath.Join(dir, ".stunts")
	run(t, nil, "build", "--manifest", manifestPath, "--out", out, "--bin", "/opt/stuntshell")
	code, stdout, _ := run(t, nil, "path", "--out", out)
	if code != ExitOK || !strings.HasPrefix(stdout, "export PATH=") {
		t.Fatalf("got code %d, output %q", code, stdout)
	}
	// Before a build there is nothing to export — say what to do instead.
	code, _, stderr := run(t, nil, "path", "--out", filepath.Join(dir, "nope"))
	if code != ExitRuntime || !strings.Contains(stderr, "stuntshell build") {
		t.Fatalf("got code %d, stderr %q", code, stderr)
	}
}

func TestActAnswersWithRuleResponseAndExitCode(t *testing.T) {
	manifestPath, logPath := stage(t)
	code, stdout, _ := act(t, manifestPath, logPath, "git", "status")
	if code != 0 || !strings.Contains(stdout, "On branch main") {
		t.Fatalf("got code %d, stdout %q", code, stdout)
	}
	// The starter git double answers unmatched argv with its default.
	code, _, stderr := act(t, manifestPath, logPath, "git", "frobnicate")
	if code != 1 || !strings.Contains(stderr, "no stunt rule matched: frobnicate") {
		t.Fatalf("default should answer: code %d, stderr %q", code, stderr)
	}
}

func TestActScriptFailsThenRecovers(t *testing.T) {
	manifestPath, logPath := stage(t)
	code, _, stderr := act(t, manifestPath, logPath, "git", "fetch", "origin")
	if code != 128 || !strings.Contains(stderr, "unable to access remote") {
		t.Fatalf("first fetch should fail like a network error: %d %q", code, stderr)
	}
	code, _, _ = act(t, manifestPath, logPath, "git", "fetch", "origin")
	if code != 0 {
		t.Fatalf("second fetch should succeed, got %d", code)
	}
}

func TestActCapturesStdinWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "m.json")
	src := `{"version": 1, "commands": {"kubectl": {"capture_stdin": true,
		"rules": [{"match": ["apply", "-f", "-"], "stdout": "applied\n"}]}}}`
	if err := os.WriteFile(manifestPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "log.jsonl")
	args := []string{"__act", "--manifest", manifestPath, "--log", logPath, "--", "kubectl", "apply", "-f", "-"}
	code, stdout, _ := run(t, strings.NewReader("kind: Pod\n"), args...)
	if code != 0 || stdout != "applied\n" {
		t.Fatalf("apply double misbehaved: %d %q", code, stdout)
	}
	code, stdout, _ = run(t, nil, "log", "--log", logPath, "--format", "json")
	if code != ExitOK || !strings.Contains(stdout, `"stdin": "kind: Pod\n"`) {
		t.Fatalf("stdin should appear in the journal: %q", stdout)
	}
}

func TestLogListsInvocationsInOrder(t *testing.T) {
	manifestPath, logPath := stage(t)
	act(t, manifestPath, logPath, "git", "status")
	act(t, manifestPath, logPath, "git", "push", "origin", "main")
	act(t, manifestPath, logPath, "git", "frobnicate")
	code, stdout, _ := run(t, nil, "log", "--log", logPath)
	if code != ExitOK {
		t.Fatal("log should succeed")
	}
	if !strings.Contains(stdout, "3 invocations") {
		t.Fatalf("count missing: %q", stdout)
	}
	statusIdx := strings.Index(stdout, "git status")
	pushIdx := strings.Index(stdout, "git push origin main")
	if statusIdx < 0 || pushIdx < 0 || statusIdx > pushIdx {
		t.Fatalf("journal order broken:\n%s", stdout)
	}
	if !strings.Contains(stdout, "(no rule)") {
		t.Fatalf("unmatched invocation should be flagged:\n%s", stdout)
	}
}

func TestLogFiltersByCommandAndEmitsJSON(t *testing.T) {
	manifestPath, logPath := stage(t)
	act(t, manifestPath, logPath, "git", "status")
	act(t, manifestPath, logPath, "kubectl", "get", "pods")
	code, stdout, _ := run(t, nil, "log", "--log", logPath, "--command", "kubectl")
	if code != ExitOK || !strings.Contains(stdout, "1 invocation\n") || strings.Contains(stdout, "git") {
		t.Fatalf("filter failed:\n%s", stdout)
	}
	_, stdout, _ = run(t, nil, "log", "--log", logPath, "--format", "json", "--command", "git")
	var records []map[string]any
	if err := json.Unmarshal([]byte(stdout), &records); err != nil {
		t.Fatalf("log --format json must be valid JSON: %v\n%s", err, stdout)
	}
	if len(records) != 1 || records[0]["command"] != "git" {
		t.Fatalf("unexpected records: %v", records)
	}
}

func TestVerifyPassesTheHappyPathInTextAndJSON(t *testing.T) {
	manifestPath, logPath := stage(t)
	act(t, manifestPath, logPath, "git", "status")
	code, stdout, _ := run(t, nil, "verify", "--manifest", manifestPath, "--log", logPath)
	if code != ExitOK || !strings.Contains(stdout, "verify: PASS") {
		t.Fatalf("got code %d:\n%s", code, stdout)
	}
	code, stdout, _ = run(t, nil, "verify", "--manifest", manifestPath, "--log", logPath, "--format", "json")
	if code != ExitOK {
		t.Fatalf("verify --format json exit %d", code)
	}
	var rep verify.Report
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("verify --format json must be valid JSON: %v", err)
	}
	if !rep.Pass || rep.Invocations != 1 || len(rep.Outcomes) != 2 {
		t.Fatalf("unexpected report: %+v", rep)
	}
}

func TestVerifyFailsAndExitsOneOnBreach(t *testing.T) {
	manifestPath, logPath := stage(t)
	act(t, manifestPath, logPath, "git", "status")
	act(t, manifestPath, logPath, "git", "push", "--force", "origin") // forbidden by expect
	code, stdout, _ := run(t, nil, "verify", "--manifest", manifestPath, "--log", logPath)
	if code != ExitFail {
		t.Fatalf("breach must exit %d, got %d", ExitFail, code)
	}
	if !strings.Contains(stdout, "FAIL") || !strings.Contains(stdout, "never force-push") {
		t.Fatalf("report should name the failed expectation:\n%s", stdout)
	}
}

func TestVerifyStrictFlagsUnmatchedInvocations(t *testing.T) {
	manifestPath, logPath := stage(t)
	act(t, manifestPath, logPath, "git", "status")
	act(t, manifestPath, logPath, "git", "frobnicate") // hits the default, unmatched
	code, stdout, _ := run(t, nil, "verify", "--manifest", manifestPath, "--log", logPath, "--strict")
	if code != ExitFail || !strings.Contains(stdout, "strict: FAIL") {
		t.Fatalf("strict should fail on the unmatched call:\n%s", stdout)
	}
}

func TestAssertPassAndFailExitCodes(t *testing.T) {
	manifestPath, logPath := stage(t)
	act(t, manifestPath, logPath, "git", "push", "origin", "main")
	code, stdout, _ := run(t, nil, "assert", "--log", logPath,
		"--command", "git", "--args", "push origin main", "--min", "1")
	if code != ExitOK || !strings.Contains(stdout, "PASS") {
		t.Fatalf("assert should pass: %d %q", code, stdout)
	}
	code, stdout, _ = run(t, nil, "assert", "--log", logPath,
		"--command", "git", "--args-glob", "push --force ...", "--max", "0")
	if code != ExitOK {
		t.Fatalf("never-force-push assert should pass: %q", stdout)
	}
	code, stdout, _ = run(t, nil, "assert", "--log", logPath,
		"--command", "git", "--args", "status")
	if code != ExitFail || !strings.Contains(stdout, "FAIL") {
		t.Fatalf("missing status call should fail: %d %q", code, stdout)
	}
}

func TestAssertValidatesItsFlags(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log.jsonl")
	cases := [][]string{
		{"assert", "--log", logPath}, // no --command
		{"assert", "--log", logPath, "--command", "git", "--args", "a", "--args-glob", "b"}, // both
		{"assert", "--log", logPath, "--command", "git", "--exactly", "1", "--min", "1"},    // exactly + min
		{"assert", "--log", logPath, "--command", "git", "--min", "3", "--max", "1"},        // inverted
	}
	for _, argv := range cases {
		if code, _, _ := run(t, nil, argv...); code != ExitUsage {
			t.Fatalf("%v should be a usage error, got %d", argv, code)
		}
	}
}

func TestResetStartsAFreshTestCase(t *testing.T) {
	manifestPath, logPath := stage(t)
	act(t, manifestPath, logPath, "git", "status")
	if code, _, _ := run(t, nil, "reset", "--log", logPath); code != ExitOK {
		t.Fatal("reset should succeed")
	}
	_, stdout, _ := run(t, nil, "log", "--log", logPath)
	if !strings.Contains(stdout, "0 invocations") {
		t.Fatalf("journal should be empty after reset:\n%s", stdout)
	}
	// And the same expectations now fail again — a clean slate.
	code, _, _ := run(t, nil, "verify", "--manifest", manifestPath, "--log", logPath)
	if code != ExitFail {
		t.Fatal("verify on the fresh journal should fail its min-1 expectation")
	}
}

func TestBadFlagsAreUsageErrors(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log.jsonl")
	if code, _, _ := run(t, nil, "log", "--log", logPath, "--format", "yaml"); code != ExitUsage {
		t.Fatal("log --format yaml should exit 2")
	}
	manifestPath, _ := stage(t)
	if code, _, _ := run(t, nil, "verify", "--manifest", manifestPath, "--log", logPath, "--format", "yaml"); code != ExitUsage {
		t.Fatal("verify --format yaml should exit 2")
	}
	// __act without its baked-in flags is a shim bug, not a double result.
	code, _, stderr := run(t, nil, "__act", "--", "git")
	if code != ExitUsage || !strings.Contains(stderr, "--manifest") {
		t.Fatalf("got code %d, stderr %q", code, stderr)
	}
}
