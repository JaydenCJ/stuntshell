// Unit tests for the argv pattern language. These pin down the exact
// semantics documented in docs/manifest.md — especially the nil-vs-empty
// distinction and rest-token behavior, which decide whether a double
// answers or falls through to its default.
package match

import "testing"

func TestTokenLiteralExactAndCaseSensitive(t *testing.T) {
	if !Token("status", "status") {
		t.Fatal("literal token should match itself")
	}
	// Doubles must never over-match: `git STATUS` is a different argv.
	if Token("status", "STATUS") {
		t.Fatal("matching must be case-sensitive")
	}
}

func TestTokenStarMatchesAnyRun(t *testing.T) {
	for _, arg := range []string{"", "main", "feature/x-1"} {
		if !Token("*", arg) {
			t.Fatalf("* should match %q", arg)
		}
	}
	if !Token("--depth=*", "--depth=1") {
		t.Fatal("--depth=* should match --depth=1")
	}
	if Token("--depth=*", "--jobs=1") {
		t.Fatal("--depth=* must not match --jobs=1")
	}
}

func TestTokenQuestionMarkMatchesExactlyOneRune(t *testing.T) {
	if !Token("v?", "v1") {
		t.Fatal("v? should match v1")
	}
	if Token("v?", "v") || Token("v?", "v12") {
		t.Fatal("? must match exactly one character")
	}
	// One character means one rune, not one byte — argv is user text.
	if !Token("?", "本") {
		t.Fatal("? should match a single multi-byte rune")
	}
}

func TestTokenBackslashEscapes(t *testing.T) {
	if !Token(`\*`, "*") {
		t.Fatal(`\* should match a literal asterisk`)
	}
	if Token(`\*`, "x") {
		t.Fatal(`\* must not act as a wildcard`)
	}
	// A dangling escape is a manifest bug; failing closed beats guessing.
	if Token(`abc\`, `abc\`) || Token(`abc\`, "abc") {
		t.Fatal("trailing lone backslash should match nothing")
	}
}

func TestTokenBacktrackingPathologicalPattern(t *testing.T) {
	// The iterative matcher must survive patterns that explode a naive
	// recursive one, and still return the right answer.
	if !Token("*a*a*a*a*b", "aaaaaaaaaaaaaaaaaaab") {
		t.Fatal("pattern should match")
	}
	if Token("*a*a*a*a*b", "aaaaaaaaaaaaaaaaaaac") {
		t.Fatal("pattern must not match a c-terminated string")
	}
}

func TestArgsNilVersusEmptyPatternList(t *testing.T) {
	// nil is the catch-all; [] demands an empty argv. The manifest maps
	// an omitted "match" to nil and "match": [] to empty, so this
	// distinction is load-bearing.
	if !Args(nil, []string{"anything", "at", "all"}) || !Args(nil, nil) {
		t.Fatal("nil pattern list is the catch-all")
	}
	if !Args([]string{}, nil) {
		t.Fatal("empty pattern should match empty argv")
	}
	if Args([]string{}, []string{"x"}) {
		t.Fatal("empty pattern must not match a non-empty argv")
	}
}

func TestArgsLengthMustMatchWithoutRest(t *testing.T) {
	if Args([]string{"push", "origin"}, []string{"push", "origin", "main"}) {
		t.Fatal("extra arguments must not match without the rest token")
	}
	if Args([]string{"push", "origin"}, []string{"push"}) {
		t.Fatal("missing arguments must not match")
	}
}

func TestArgsRestConsumesRemainder(t *testing.T) {
	pattern := []string{"push", Rest}
	if !Args(pattern, []string{"push"}) {
		t.Fatal("rest should match zero remaining arguments")
	}
	if !Args(pattern, []string{"push", "origin", "main", "--tags"}) {
		t.Fatal("rest should match many remaining arguments")
	}
	if Args(pattern, []string{"pull", "origin"}) {
		t.Fatal("head tokens before rest must still match")
	}
	if !Args([]string{Rest}, nil) || !Args([]string{Rest}, []string{"a", "b"}) {
		t.Fatal("a bare rest token matches any argv")
	}
}

func TestArgsGlobAndRestCombine(t *testing.T) {
	pattern := []string{"clone", "--depth=*", Rest}
	if !Args(pattern, []string{"clone", "--depth=1", "https://example.test/repo.git"}) {
		t.Fatal("glob head + rest tail should match")
	}
	if Args(pattern, []string{"clone", "--bare", "https://example.test/repo.git"}) {
		t.Fatal("glob head must be enforced before rest")
	}
}
