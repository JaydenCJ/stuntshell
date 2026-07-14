// Unit tests for the invocation journal: append/read round-trips, the
// missing-file convention, corruption reporting, and the rule counter that
// script sequencing depends on.
package journal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func journalPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "invocations.jsonl")
}

func TestAppendThenReadRoundTrips(t *testing.T) {
	path := journalPath(t)
	in := Record{Command: "git", Args: []string{"push", "origin", "main"},
		Rule: 1, Matched: true, Exit: 0, Dir: "/work"}
	if err := Append(path, in); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	r := got[0]
	if r.Command != "git" || len(r.Args) != 3 || r.Args[2] != "main" ||
		r.Rule != 1 || !r.Matched || r.Exit != 0 || r.Dir != "/work" {
		t.Fatalf("round-trip mangled the record: %+v", r)
	}
	if r.Argv() != "git push origin main" {
		t.Fatalf("display line wrong: %q", r.Argv())
	}
}

func TestReadMissingFileIsEmptyJournal(t *testing.T) {
	// Verifying before anything ran is a legitimate (failing) assertion
	// run, so a missing journal must not be a runtime error.
	got, err := Read(filepath.Join(t.TempDir(), "never-created.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal("missing file should read as empty")
	}
}

func TestAppendPreservesOrder(t *testing.T) {
	path := journalPath(t)
	for i, cmd := range []string{"git", "kubectl", "git"} {
		if err := Append(path, Record{Command: cmd, Rule: i}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Command != "git" || got[1].Command != "kubectl" || got[2].Command != "git" {
		t.Fatalf("journal order broken: %+v", got)
	}
}

func TestAppendNormalizesNilArgs(t *testing.T) {
	path := journalPath(t)
	if err := Append(path, Record{Command: "true"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Consumers of the JSONL (jq scripts, CI) should always see an array.
	if !strings.Contains(string(data), `"args":[]`) {
		t.Fatalf("nil args should serialize as []: %s", data)
	}
}

func TestReadSkipsBlankLinesAndReportsCorruption(t *testing.T) {
	path := journalPath(t)
	content := `{"command":"git","args":[],"rule":0,"matched":true,"exit":0}` + "\n\n" +
		`{"command":"git","args":["status"],"rule":0,"matched":true,"exit":0}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 records, got %d", len(got))
	}
	// A corrupt line must produce a line-numbered error, not silence.
	if err := os.WriteFile(path, []byte(content+"not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Read(path)
	if err == nil || !strings.Contains(err.Error(), "line 4") {
		t.Fatalf("want a line-numbered error, got %v", err)
	}
}

func TestReadSurvivesLargeStdinRecords(t *testing.T) {
	// Captured stdin can be 64 KiB; the reader must not choke on lines far
	// beyond bufio.Scanner's default token size.
	path := journalPath(t)
	big := strings.Repeat("x", 64*1024)
	if err := Append(path, Record{Command: "kubectl", Stdin: big}); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Stdin) != 64*1024 {
		t.Fatal("large stdin record should round-trip intact")
	}
}

func TestResetTruncatesAndCreates(t *testing.T) {
	path := journalPath(t)
	if err := Append(path, Record{Command: "git"}); err != nil {
		t.Fatal(err)
	}
	if err := Reset(path); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil || len(got) != 0 {
		t.Fatalf("reset should leave an empty journal, got %d records (%v)", len(got), err)
	}
	// Resetting a journal that never existed must also work.
	fresh := filepath.Join(t.TempDir(), "fresh.jsonl")
	if err := Reset(fresh); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("reset should create the file")
	}
}

func TestCountRuleCountsCommandAndRulePairs(t *testing.T) {
	records := []Record{
		{Command: "git", Rule: 0},
		{Command: "git", Rule: 1},
		{Command: "git", Rule: 0},
		{Command: "kubectl", Rule: 0},
		{Command: "git", Rule: -1},
	}
	if n := CountRule(records, "git", 0); n != 2 {
		t.Fatalf("want 2, got %d", n)
	}
	if n := CountRule(records, "git", -1); n != 1 {
		t.Fatalf("default fallbacks should be countable, got %d", n)
	}
	if n := CountRule(records, "curl", 0); n != 0 {
		t.Fatalf("want 0, got %d", n)
	}
}

func TestRecordArgvOfBareInvocation(t *testing.T) {
	bare := Record{Command: "true"}
	if bare.Argv() != "true" {
		t.Fatalf("got %q", bare.Argv())
	}
}
