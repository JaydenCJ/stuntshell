// Unit tests for manifest loading and validation. A testing tool's own
// config errors must fail loudly at load time — several cases here exist
// purely to prove a typo cannot silently produce a do-nothing double.
package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimal is the smallest valid manifest.
const minimal = `{"version": 1, "commands": {"git": {"rules": [{"match": ["status"]}]}}}`

func parse(t *testing.T, src string) *Manifest {
	t.Helper()
	m, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return m
}

func parseErr(t *testing.T, src, wantSubstr string) {
	t.Helper()
	_, err := Parse(strings.NewReader(src))
	if err == nil {
		t.Fatalf("Parse should fail, want error containing %q", wantSubstr)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Fatalf("error %q does not contain %q", err, wantSubstr)
	}
}

func TestParseMinimalManifest(t *testing.T) {
	m := parse(t, minimal)
	if len(m.Commands) != 1 || m.Commands["git"] == nil {
		t.Fatal("git command should be present")
	}
}

func TestParseRejectsUnknownFieldsAndTrailingData(t *testing.T) {
	// "stdotu" instead of "stdout" must be a load error, not a no-op.
	parseErr(t, `{"version": 1, "commands": {"git": {"rules": [{"stdotu": "x"}]}}}`, "stdotu")
	parseErr(t, minimal+` {"version": 1}`, "trailing data")
}

func TestParseRejectsStructurallyInvalidDocuments(t *testing.T) {
	parseErr(t, `{"version": 2, "commands": {"git": {}}}`, "unsupported manifest version 2")
	parseErr(t, `{"version": 1}`, "at least one command")
	parseErr(t, `{"version": 1, "commands": {"git": null}}`, "null")
}

func TestParseRejectsBadCommandNames(t *testing.T) {
	for _, name := range []string{"a/b", `a\b`, "a b", "..", "", "stuntshell"} {
		src := `{"version": 1, "commands": {"` + name + `": {}}}`
		if _, err := Parse(strings.NewReader(src)); err == nil {
			t.Fatalf("command name %q should be rejected", name)
		}
	}
}

func TestParseRejectsRestTokenMidPattern(t *testing.T) {
	parseErr(t, `{"version": 1, "commands": {"git": {"rules": [{"match": ["...", "push"]}]}}}`,
		"final pattern token")
}

func TestParseScriptExcludesInlineResponse(t *testing.T) {
	parseErr(t, `{"version": 1, "commands": {"git": {"rules": [
		{"match": ["fetch"], "stdout": "x", "script": [{"exit": 1}]}]}}}`,
		"not both")
	m := parse(t, `{"version": 1, "commands": {"git": {"rules": [
		{"match": ["fetch"], "script": [{"exit": 1}, {"exit": 0}]}]}}}`)
	if len(m.Commands["git"].Rules[0].Script) != 2 {
		t.Fatal("a script-only rule should parse, keeping both takes")
	}
}

func TestParseRejectsExpectationForUndeclaredCommand(t *testing.T) {
	parseErr(t, `{"version": 1, "commands": {"git": {}},
		"expect": [{"command": "kubectl"}]}`, `unknown command "kubectl"`)
}

func TestParseRejectsExpectationWithArgsAndArgsGlob(t *testing.T) {
	parseErr(t, `{"version": 1, "commands": {"git": {}},
		"expect": [{"command": "git", "args": ["a"], "args_glob": ["a"]}]}`, "not both")
}

func TestParseRejectsIncoherentBounds(t *testing.T) {
	parseErr(t, `{"version": 1, "commands": {"git": {}},
		"expect": [{"command": "git", "exactly": 1, "min": 1}]}`, `excludes "min"`)
	parseErr(t, `{"version": 1, "commands": {"git": {}},
		"expect": [{"command": "git", "min": -1}]}`, "must be ≥ 0")
	parseErr(t, `{"version": 1, "commands": {"git": {}},
		"expect": [{"command": "git", "min": 3, "max": 1}]}`, "exceeds")
}

func TestMatchNilVersusEmptySurvivesDecoding(t *testing.T) {
	// The JSON distinction between an omitted "match" and "match": [] is
	// load-bearing: catch-all versus only-empty-argv.
	m := parse(t, `{"version": 1, "commands": {"git": {"rules": [
		{"stdout": "any"}, {"match": [], "stdout": "none"}]}}}`)
	rules := m.Commands["git"].Rules
	if rules[0].Match != nil {
		t.Fatal("omitted match should decode as nil (catch-all)")
	}
	if rules[1].Match == nil || len(rules[1].Match) != 0 {
		t.Fatal(`"match": [] should decode as empty, non-nil`)
	}
}

func TestExpectationBounds(t *testing.T) {
	one, three := 1, 3
	cases := []struct {
		name    string
		e       Expectation
		min     int
		max     int
		bounded bool
	}{
		{"bare means at least once", Expectation{}, 1, 0, false},
		{"exactly pins both bounds", Expectation{Exactly: &three}, 3, 3, true},
		{"max only means may be absent", Expectation{Max: &three}, 0, 3, true},
		{"min and max", Expectation{Min: &one, Max: &three}, 1, 3, true},
	}
	for _, c := range cases {
		min, max, bounded := c.e.Bounds()
		if min != c.min || bounded != c.bounded || (bounded && max != c.max) {
			t.Fatalf("%s: got min=%d max=%d bounded=%v", c.name, min, max, bounded)
		}
	}
}

func TestExpectationLabel(t *testing.T) {
	e := Expectation{Command: "git", ArgsGlob: []string{"push", "..."}}
	if e.Label() != "git push ..." {
		t.Fatalf("got %q", e.Label())
	}
	e.Description = "never force-push"
	if e.Label() != "never force-push" {
		t.Fatal("description should win")
	}
	bare := Expectation{Command: "kubectl"}
	if bare.Label() != "kubectl …" {
		t.Fatalf("got %q", bare.Label())
	}
}

func TestResponseExitCodeDefaultsToZero(t *testing.T) {
	if (Response{}).ExitCode() != 0 {
		t.Fatal("nil exit should mean 0")
	}
	seven := 7
	if (Response{Exit: &seven}).ExitCode() != 7 {
		t.Fatal("explicit exit should be honored")
	}
}

func TestCommandNamesAreSorted(t *testing.T) {
	m := parse(t, `{"version": 1, "commands": {"kubectl": {}, "curl": {}, "git": {}}}`)
	names := m.CommandNames()
	want := []string{"curl", "git", "kubectl"}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("got %v, want %v", names, want)
		}
	}
}

func TestLoadReadsDiskNamesFilesInErrorsAndRejectsAbsentFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stuntshell.json")
	if err := os.WriteFile(path, []byte(minimal), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"version": 9}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(bad)
	if err == nil || !strings.Contains(err.Error(), "bad.json") {
		t.Fatalf("error should name the file, got %v", err)
	}
	if _, err := Load(filepath.Join(dir, "absent.json")); err == nil {
		t.Fatal("missing manifest must be an error")
	}
}
