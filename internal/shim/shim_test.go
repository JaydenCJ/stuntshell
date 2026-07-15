// Unit tests for shim generation: shell quoting (the one place a bug
// becomes arbitrary command execution in someone's test suite), generated
// script shape, permissions, and build-idempotence guarantees.
package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/JaydenCJ/stuntshell/internal/journal"
	"github.com/JaydenCJ/stuntshell/internal/manifest"
)

func testManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse(strings.NewReader(
		`{"version": 1, "commands": {"kubectl": {}, "git": {}}}`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestQuotePlainAndEmbeddedSingleQuote(t *testing.T) {
	if Quote("git") != "'git'" {
		t.Fatalf("got %q", Quote("git"))
	}
	// /tmp/o'brien must survive: close, escape, reopen.
	want := `'/tmp/o'\''brien'`
	if got := Quote("/tmp/o'brien"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestQuoteNeutralizesShellMetacharacters(t *testing.T) {
	// Everything between single quotes is literal in POSIX sh; the quoted
	// form must simply preserve the raw string inside one pair of quotes.
	hostile := `$(rm -rf /tmp/x); echo "$HOME" | tee &`
	got := Quote(hostile)
	if got != "'"+hostile+"'" {
		t.Fatalf("got %q", got)
	}
}

func TestScriptShape(t *testing.T) {
	s := Script("/opt/stuntshell", "/work/stuntshell.json", "/work/log.jsonl", "git")
	if !strings.HasPrefix(s, "#!/usr/bin/env sh\n") {
		t.Fatal("shim must start with a sh shebang")
	}
	if !strings.Contains(s, `exec '/opt/stuntshell' __act --manifest '/work/stuntshell.json' --log '/work/log.jsonl' -- 'git' "$@"`) {
		t.Fatalf("exec line malformed:\n%s", s)
	}
	if !strings.HasSuffix(s, "\n") {
		t.Fatal("shim must end with a newline")
	}
}

func TestBuildWritesOneExecutablePerCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	dir := t.TempDir()
	res, err := Build(testManifest(t), filepath.Join(dir, ".stunts"), "/opt/stuntshell", "/work/m.json", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Commands) != 2 || res.Commands[0] != "git" || res.Commands[1] != "kubectl" {
		t.Fatalf("commands should be sorted: %v", res.Commands)
	}
	for _, name := range res.Commands {
		info, err := os.Stat(filepath.Join(res.BinDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("%s must be executable, mode %v", name, info.Mode())
		}
	}
	// The journal defaults to sitting next to bin/, and build creates it.
	want := filepath.Join(dir, ".stunts", "invocations.jsonl")
	if res.LogPath != want {
		t.Fatalf("got %q, want %q", res.LogPath, want)
	}
	if _, err := os.Stat(res.LogPath); err != nil {
		t.Fatal("build should create the journal file")
	}
}

func TestBuildBakesAbsolutePaths(t *testing.T) {
	// Relative inputs would break the doubles the moment the program
	// under test changes directory.
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	res, err := Build(testManifest(t), ".stunts", "stuntshell-bin", "m.json", "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(res.BinDir, "git"))
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{"stuntshell-bin", "m.json"} {
		idx := strings.Index(string(data), needle)
		if idx < 1 || string(data)[idx-1] == '\'' {
			// The character right before the name must be a path
			// separator, i.e. the path was absolutized.
			t.Fatalf("%s not absolutized in shim:\n%s", needle, data)
		}
	}
	if !filepath.IsAbs(res.BinDir) || !filepath.IsAbs(res.LogPath) {
		t.Fatal("result paths must be absolute")
	}
}

func TestBuildPreservesExistingJournal(t *testing.T) {
	// Rebuilding doubles mid-suite must never destroy recorded evidence.
	dir := t.TempDir()
	out := filepath.Join(dir, ".stunts")
	res, err := Build(testManifest(t), out, "/opt/stuntshell", "/work/m.json", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(res.LogPath, journal.Record{Command: "git"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(testManifest(t), out, "/opt/stuntshell", "/work/m.json", ""); err != nil {
		t.Fatal(err)
	}
	records, err := journal.Read(res.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("rebuild truncated the journal: %d records", len(records))
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	m := testManifest(t)
	logPath := filepath.Join(dir, "log.jsonl")
	first, err := Build(m, filepath.Join(dir, "a"), "/opt/stuntshell", "/work/m.json", logPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(m, filepath.Join(dir, "b"), "/opt/stuntshell", "/work/m.json", logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range first.Commands {
		a, _ := os.ReadFile(filepath.Join(first.BinDir, name))
		b, _ := os.ReadFile(filepath.Join(second.BinDir, name))
		if string(a) != string(b) {
			t.Fatalf("shim %s differs between identical builds", name)
		}
	}
}

func TestExportLineIsEvalReady(t *testing.T) {
	got := ExportLine("/work/.stunts/bin")
	if got != `export PATH='/work/.stunts/bin':"$PATH"` {
		t.Fatalf("got %q", got)
	}
}
