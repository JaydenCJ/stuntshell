// Unit tests for the response placeholder language. Placeholders are the
// only dynamism a double has, so their edge cases (unknown keys, unclosed
// braces, out-of-range indexes) must be boring and predictable.
package expand

import "testing"

func ctx() Context {
	return Context{Command: "git", Args: []string{"push", "origin", "main"}, Nth: 2}
}

func TestExpandPlainTextPassesThrough(t *testing.T) {
	if got := Expand("nothing to see here\n", ctx()); got != "nothing to see here\n" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandCorePlaceholders(t *testing.T) {
	cases := []struct{ in, want string }{
		{"{cmd} ran", "git ran"},
		{"saw: {argv}", "saw: push origin main"},
		{"{0}/{1}/{2}", "push/origin/main"},
		{"{#} args", "3 args"},
		{"attempt {nth}", "attempt 2"},
	}
	for _, c := range cases {
		if got := Expand(c.in, ctx()); got != c.want {
			t.Fatalf("Expand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Out-of-range indexes expand to "": echoing "{9}" verbatim would
	// leak template syntax into the program under test, and an empty
	// string is what a missing argument looks like.
	if got := Expand("[{9}]", ctx()); got != "[]" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandUnknownPlaceholderLeftVerbatim(t *testing.T) {
	// A manifest typo must be visible in the output, not silently eaten.
	if got := Expand("{bogus} and {cmd}", ctx()); got != "{bogus} and git" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandSignedNumberIsNotAnIndex(t *testing.T) {
	// strconv.Atoi would accept "+1"; the placeholder grammar must not.
	if got := Expand("{+1}", ctx()); got != "{+1}" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandEscapedBraces(t *testing.T) {
	if got := Expand("json: {{\"k\": 1}}", ctx()); got != `json: {"k": 1}` {
		t.Fatalf("got %q", got)
	}
}

func TestExpandUnclosedBraceIsLiteral(t *testing.T) {
	if got := Expand("dangling {cmd", ctx()); got != "dangling {cmd" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandEmptyArgsJoinToEmptyArgv(t *testing.T) {
	c := Context{Command: "true", Nth: 1}
	if got := Expand("<{argv}>", c); got != "<>" {
		t.Fatalf("got %q", got)
	}
}
