// Package match implements the argv pattern language shared by manifest
// rules and expectations: per-token globs plus a rest token. It is pure and
// allocation-light so a double can dispatch in microseconds.
package match

// Rest is the pattern token that, in final position, matches the remainder
// of argv (zero or more arguments). Anywhere else it is rejected by
// manifest validation.
const Rest = "..."

// Args reports whether the pattern list matches an argument vector.
//
// Semantics (documented in docs/manifest.md):
//   - a nil pattern list matches ANY argv (the catch-all / "match omitted"),
//   - an empty, non-nil pattern list matches only an empty argv,
//   - otherwise patterns are matched positionally with Token, and argv must
//     have exactly the same length — unless the final pattern is Rest, which
//     consumes the (possibly empty) remainder.
func Args(patterns, args []string) bool {
	if patterns == nil {
		return true
	}
	n := len(patterns)
	if n > 0 && patterns[n-1] == Rest {
		head := patterns[:n-1]
		if len(args) < len(head) {
			return false
		}
		return matchAll(head, args[:len(head)])
	}
	if len(args) != n {
		return false
	}
	return matchAll(patterns, args)
}

func matchAll(patterns, args []string) bool {
	for i, p := range patterns {
		if !Token(p, args[i]) {
			return false
		}
	}
	return true
}

// Token reports whether a single glob pattern matches a single argument.
//
//   - `*` matches any run of characters (including none),
//   - `?` matches exactly one character,
//   - `\x` matches the literal character x (so `\*` is a literal asterisk).
//
// A trailing lone backslash matches nothing; escapes must precede a
// character. Matching is exact otherwise — no character classes, no case
// folding — because doubles should never accidentally over-match.
func Token(pattern, arg string) bool {
	return globMatch([]rune(pattern), []rune(arg))
}

// globMatch is an iterative glob matcher with single-star backtracking,
// avoiding the exponential blowup a naive recursive matcher hits on
// pathological patterns like "*a*a*a*a*b".
func globMatch(p, s []rune) bool {
	starP, starS := -1, 0
	i, j := 0, 0
	for j < len(s) {
		stepped := false
		if i < len(p) {
			switch p[i] {
			case '*':
				starP, starS = i, j
				i++
				continue
			case '\\':
				if i+1 < len(p) && p[i+1] == s[j] {
					i += 2
					j++
					stepped = true
				}
			case '?':
				i++
				j++
				stepped = true
			default:
				if p[i] == s[j] {
					i++
					j++
					stepped = true
				}
			}
		}
		if stepped {
			continue
		}
		// Mismatch: retry from the last star, consuming one more character.
		if starP >= 0 {
			starS++
			i = starP + 1
			j = starS
			continue
		}
		return false
	}
	// Argument exhausted: only trailing stars may remain in the pattern.
	for i < len(p) && p[i] == '*' {
		i++
	}
	return i == len(p)
}
