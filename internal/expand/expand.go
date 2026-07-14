// Package expand implements the placeholder mini-language available in a
// response's stdout and stderr, so a double can echo back what it was
// invoked with — enough dynamism for realistic doubles without turning the
// manifest into a programming language.
package expand

import (
	"strconv"
	"strings"
)

// Context carries the invocation values placeholders can reference.
type Context struct {
	Command string   // the double's name, e.g. "git"
	Args    []string // argv after the command name
	Nth     int      // 1-based ordinal of this invocation for its rule
}

// Expand substitutes placeholders in s:
//
//	{cmd}   the command name
//	{argv}  all arguments joined with single spaces
//	{0}…    the argument at that zero-based index ("" when out of range)
//	{#}     the number of arguments
//	{nth}   the 1-based per-rule invocation ordinal
//	{{ }}   literal braces
//
// Unknown placeholders are left verbatim (a manifest typo shows up in the
// double's output instead of silently vanishing), and an unclosed brace is
// treated as literal text.
func Expand(s string, ctx Context) string {
	if !strings.ContainsAny(s, "{}") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c == '{' {
			if i+1 < len(s) && s[i+1] == '{' {
				b.WriteByte('{')
				i += 2
				continue
			}
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				b.WriteString(s[i:])
				break
			}
			key := s[i+1 : i+end]
			if v, ok := lookup(key, ctx); ok {
				b.WriteString(v)
			} else {
				b.WriteString(s[i : i+end+1])
			}
			i += end + 1
			continue
		}
		if c == '}' && i+1 < len(s) && s[i+1] == '}' {
			b.WriteByte('}')
			i += 2
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

func lookup(key string, ctx Context) (string, bool) {
	switch key {
	case "cmd":
		return ctx.Command, true
	case "argv":
		return strings.Join(ctx.Args, " "), true
	case "#":
		return strconv.Itoa(len(ctx.Args)), true
	case "nth":
		return strconv.Itoa(ctx.Nth), true
	}
	if !isDigits(key) {
		return "", false
	}
	n, err := strconv.Atoi(key)
	if err != nil {
		return "", false
	}
	if n < len(ctx.Args) {
		return ctx.Args[n], true
	}
	return "", true
}

// isDigits rejects signs and empty strings, which strconv.Atoi would accept
// ("+1") or that would be ambiguous ("" is not an index).
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
