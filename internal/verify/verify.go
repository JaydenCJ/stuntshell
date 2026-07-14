// Package verify evaluates invocation assertions against the journal: the
// manifest's expect list (counts, argv patterns, ordering) plus the strict
// no-unmatched-invocations check. It is pure — callers load the manifest
// and journal, verify only judges.
package verify

import (
	"fmt"

	"github.com/JaydenCJ/stuntshell/internal/journal"
	"github.com/JaydenCJ/stuntshell/internal/manifest"
	"github.com/JaydenCJ/stuntshell/internal/match"
)

// Outcome is the verdict for one expectation.
type Outcome struct {
	Label string `json:"label"`
	Count int    `json:"count"`
	// First is the zero-based journal index of the first matching
	// invocation, or -1 when nothing matched.
	First int    `json:"first"`
	OK    bool   `json:"ok"`
	Want  string `json:"want"` // human-readable bound, e.g. "min 1" or "exactly 2"
}

// Report is the full verification result.
type Report struct {
	Invocations int       `json:"invocations"`
	Outcomes    []Outcome `json:"outcomes"`
	// Unmatched lists zero-based journal indexes that hit no rule; it
	// fails the report only in strict mode.
	Unmatched []int `json:"unmatched,omitempty"`
	Strict    bool  `json:"strict"`
	// OrderViolation explains a broken `ordered` constraint ("" = ok).
	OrderViolation string `json:"order_violation,omitempty"`
	Pass           bool   `json:"pass"`
}

// Run evaluates every expectation in m against the journal records.
// Strictness is the OR of the manifest's strict field and the flag.
func Run(m *manifest.Manifest, records []journal.Record, strict bool) Report {
	rep := Report{Invocations: len(records), Strict: strict || m.Strict}
	pass := true
	for _, e := range m.Expect {
		out := Eval(e, records)
		pass = pass && out.OK
		rep.Outcomes = append(rep.Outcomes, out)
	}
	for i, r := range records {
		if !r.Matched {
			rep.Unmatched = append(rep.Unmatched, i)
		}
	}
	if rep.Strict && len(rep.Unmatched) > 0 {
		pass = false
	}
	if m.Ordered {
		if v := orderViolation(m.Expect, rep.Outcomes); v != "" {
			rep.OrderViolation = v
			pass = false
		}
	}
	rep.Pass = pass
	return rep
}

// Eval judges a single expectation — also the engine behind `assert`.
func Eval(e manifest.Expectation, records []journal.Record) Outcome {
	out := Outcome{Label: e.Label(), First: -1}
	for i, r := range records {
		if Matches(e, r) {
			if out.First < 0 {
				out.First = i
			}
			out.Count++
		}
	}
	min, max, bounded := e.Bounds()
	out.Want = wantString(min, max, bounded)
	out.OK = out.Count >= min && (!bounded || out.Count <= max)
	return out
}

// Matches reports whether one journal record satisfies the expectation's
// command and argv pattern.
func Matches(e manifest.Expectation, r journal.Record) bool {
	if r.Command != e.Command {
		return false
	}
	switch {
	case e.Args != nil:
		if len(e.Args) != len(r.Args) {
			return false
		}
		for i, a := range e.Args {
			if r.Args[i] != a {
				return false
			}
		}
		return true
	case e.ArgsGlob != nil:
		return match.Args(e.ArgsGlob, r.Args)
	default:
		return true
	}
}

// orderViolation checks that expectations requiring at least one call were
// first satisfied in listed order. Expectations that may legally be absent
// (effective min 0) are skipped; two expectations may share a first match
// (>= not >), since overlapping patterns can hit the same invocation.
func orderViolation(expect []manifest.Expectation, outcomes []Outcome) string {
	prev := -1
	prevLabel := ""
	for i, out := range outcomes {
		min, _, _ := expect[i].Bounds()
		if min == 0 || out.First < 0 {
			continue
		}
		if out.First < prev {
			return fmt.Sprintf("%q (invocation %d) ran before %q (invocation %d)",
				out.Label, out.First+1, prevLabel, prev+1)
		}
		prev = out.First
		prevLabel = out.Label
	}
	return ""
}

func wantString(min, max int, bounded bool) string {
	switch {
	case bounded && min == max:
		return fmt.Sprintf("exactly %d", min)
	case bounded && min > 0:
		return fmt.Sprintf("min %d, max %d", min, max)
	case bounded:
		return fmt.Sprintf("max %d", max)
	default:
		return fmt.Sprintf("min %d", min)
	}
}
