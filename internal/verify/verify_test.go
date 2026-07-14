// Unit tests for expectation evaluation: count bounds, exact-vs-glob argv
// matching, the strict unmatched-invocation check, and ordering. These are
// the assertions users trust to fail their CI, so both directions (pass
// AND fail) are pinned for every feature.
package verify

import (
	"strings"
	"testing"

	"github.com/JaydenCJ/stuntshell/internal/journal"
	"github.com/JaydenCJ/stuntshell/internal/manifest"
)

func load(t *testing.T, src string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return m
}

// rec builds a matched journal record.
func rec(cmd string, args ...string) journal.Record {
	return journal.Record{Command: cmd, Args: args, Matched: true}
}

func TestBareExpectationMeansCalledAtLeastOnce(t *testing.T) {
	e := manifest.Expectation{Command: "git"}
	if out := Eval(e, nil); out.OK {
		t.Fatal("no invocations should fail a bare expectation")
	}
	if out := Eval(e, []journal.Record{rec("git", "status")}); !out.OK {
		t.Fatal("one invocation should satisfy a bare expectation")
	}
}

func TestMaxZeroExpressesNever(t *testing.T) {
	zero := 0
	e := manifest.Expectation{Command: "git", ArgsGlob: []string{"push", "--force", "..."}, Max: &zero}
	if out := Eval(e, nil); !out.OK {
		t.Fatal("never-called should pass a max-0 expectation")
	}
	bad := []journal.Record{rec("git", "push", "--force", "origin")}
	if out := Eval(e, bad); out.OK {
		t.Fatal("a force-push must fail the max-0 expectation")
	}
}

func TestExactlyBoundsBothSides(t *testing.T) {
	two := 2
	e := manifest.Expectation{Command: "git", Args: []string{"status"}, Exactly: &two}
	one := []journal.Record{rec("git", "status")}
	three := []journal.Record{rec("git", "status"), rec("git", "status"), rec("git", "status")}
	if Eval(e, one).OK || Eval(e, three).OK {
		t.Fatal("exactly 2 must reject 1 and 3")
	}
	if !Eval(e, three[:2]).OK {
		t.Fatal("exactly 2 should accept 2")
	}
}

func TestExactArgsMatchTokenForToken(t *testing.T) {
	e := manifest.Expectation{Command: "git", Args: []string{"push", "origin", "main"}}
	records := []journal.Record{
		rec("git", "push", "origin", "main"),
		rec("git", "push", "origin", "dev"),      // wrong token
		rec("git", "push", "origin"),             // wrong length
		rec("kubectl", "push", "origin", "main"), // wrong command
	}
	out := Eval(e, records)
	if out.Count != 1 {
		t.Fatalf("want count 1, got %d", out.Count)
	}
}

func TestExactArgsAreNotGlobsButArgsGlobIs(t *testing.T) {
	// "args" is literal by contract; a * must only match a literal *.
	e := manifest.Expectation{Command: "git", Args: []string{"push", "*"}}
	if Eval(e, []journal.Record{rec("git", "push", "main")}).OK {
		t.Fatal(`literal "*" must not glob-match`)
	}
	if !Eval(e, []journal.Record{rec("git", "push", "*")}).OK {
		t.Fatal(`literal "*" should match a literal *`)
	}
	g := manifest.Expectation{Command: "git", ArgsGlob: []string{"push", "origin", "*"}}
	if !Eval(g, []journal.Record{rec("git", "push", "origin", "release-7")}).OK {
		t.Fatal("glob expectation should match")
	}
}

func TestEmptyArgsMatchesOnlyBareInvocation(t *testing.T) {
	e := manifest.Expectation{Command: "make", Args: []string{}}
	records := []journal.Record{rec("make"), rec("make", "test")}
	if out := Eval(e, records); out.Count != 1 {
		t.Fatalf(`"args": [] should match only the bare call, got %d`, out.Count)
	}
}

func TestRunAggregatesAllExpectations(t *testing.T) {
	m := load(t, `{"version": 1, "commands": {"git": {}},
		"expect": [
			{"command": "git", "args": ["status"]},
			{"command": "git", "args_glob": ["push", "--force", "..."], "max": 0}
		]}`)
	records := []journal.Record{rec("git", "status")}
	rep := Run(m, records, false)
	if !rep.Pass || len(rep.Outcomes) != 2 {
		t.Fatalf("both expectations should pass: %+v", rep)
	}
	rep = Run(m, append(records, rec("git", "push", "--force", "origin")), false)
	if rep.Pass || rep.Outcomes[1].OK {
		t.Fatal("the force-push must flip the report to FAIL")
	}
}

func TestStrictFailsOnUnmatchedInvocations(t *testing.T) {
	m := load(t, `{"version": 1, "commands": {"git": {}}}`)
	records := []journal.Record{
		rec("git", "status"),
		{Command: "git", Args: []string{"frobnicate"}, Matched: false, Rule: -1},
	}
	if rep := Run(m, records, false); !rep.Pass {
		t.Fatal("non-strict run should tolerate unmatched invocations")
	}
	rep := Run(m, records, true)
	if rep.Pass || len(rep.Unmatched) != 1 || rep.Unmatched[0] != 1 {
		t.Fatalf("strict run should fail and name index 1: %+v", rep)
	}
}

func TestManifestStrictFieldForcesStrictness(t *testing.T) {
	m := load(t, `{"version": 1, "strict": true, "commands": {"git": {}}}`)
	records := []journal.Record{{Command: "git", Matched: false, Rule: -1}}
	if rep := Run(m, records, false); rep.Pass {
		t.Fatal(`"strict": true in the manifest must apply without --strict`)
	}
}

func TestOrderedPassesWhenFirstMatchesAreInListedOrder(t *testing.T) {
	m := load(t, `{"version": 1, "ordered": true, "commands": {"git": {}},
		"expect": [
			{"command": "git", "args": ["fetch"]},
			{"command": "git", "args": ["push"]}
		]}`)
	records := []journal.Record{rec("git", "fetch"), rec("git", "push")}
	if rep := Run(m, records, false); !rep.Pass || rep.OrderViolation != "" {
		t.Fatalf("in-order journal should pass: %+v", rep)
	}
}

func TestOrderedFailsWhenJournalOrderIsReversed(t *testing.T) {
	m := load(t, `{"version": 1, "ordered": true, "commands": {"git": {}},
		"expect": [
			{"command": "git", "args": ["fetch"]},
			{"command": "git", "args": ["push"]}
		]}`)
	records := []journal.Record{rec("git", "push"), rec("git", "fetch")}
	rep := Run(m, records, false)
	if rep.Pass || rep.OrderViolation == "" {
		t.Fatal("push-before-fetch must violate ordering")
	}
	if !strings.Contains(rep.OrderViolation, "git fetch") {
		t.Fatalf("violation should name the expectations: %q", rep.OrderViolation)
	}
}

func TestOrderedSkipsOptionalExpectations(t *testing.T) {
	// A max-0 ("never") expectation sits mid-list; with zero matches it
	// must not break the ordering chain around it.
	m := load(t, `{"version": 1, "ordered": true, "commands": {"git": {}},
		"expect": [
			{"command": "git", "args": ["fetch"]},
			{"command": "git", "args_glob": ["push", "--force", "..."], "max": 0},
			{"command": "git", "args": ["push"]}
		]}`)
	records := []journal.Record{rec("git", "fetch"), rec("git", "push")}
	if rep := Run(m, records, false); !rep.Pass {
		t.Fatalf("optional expectation must not affect ordering: %+v", rep)
	}
}

func TestOutcomeReportsCountFirstAndWant(t *testing.T) {
	e := manifest.Expectation{Command: "git", Args: []string{"status"}}
	records := []journal.Record{rec("git", "push"), rec("git", "status"), rec("git", "status")}
	out := Eval(e, records)
	if out.Count != 2 || out.First != 1 {
		t.Fatalf("count/first wrong: %+v", out)
	}
	// The want strings are user-facing report text; pin every shape.
	one, three := 1, 3
	cases := []struct {
		e    manifest.Expectation
		want string
	}{
		{manifest.Expectation{Command: "git"}, "min 1"},
		{manifest.Expectation{Command: "git", Exactly: &three}, "exactly 3"},
		{manifest.Expectation{Command: "git", Max: &three}, "max 3"},
		{manifest.Expectation{Command: "git", Min: &one, Max: &three}, "min 1, max 3"},
	}
	for _, c := range cases {
		if got := Eval(c.e, nil).Want; got != c.want {
			t.Fatalf("got %q, want %q", got, c.want)
		}
	}
}
