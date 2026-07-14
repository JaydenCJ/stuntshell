// Package actor is the runtime behind every generated double: it resolves
// an invocation against the manifest (first matching rule wins), consults
// the journal for script sequencing, records the invocation, and returns
// what the double must print and how it must exit.
package actor

import (
	"fmt"

	"github.com/JaydenCJ/stuntshell/internal/expand"
	"github.com/JaydenCJ/stuntshell/internal/journal"
	"github.com/JaydenCJ/stuntshell/internal/manifest"
	"github.com/JaydenCJ/stuntshell/internal/match"
)

// Invocation is one call into a double, as observed by the shim.
type Invocation struct {
	Command        string
	Args           []string
	Dir            string
	Stdin          string
	StdinTruncated bool
}

// Result is the resolved behavior for one invocation.
type Result struct {
	Stdout  string
	Stderr  string
	Exit    int
	Rule    int  // matched rule index, -1 for the default response
	Matched bool // false when the invocation fell through to the default
}

// Act resolves inv, appends its journal record, and returns the response.
//
// Script sequencing note: the "nth match of this rule" counter is derived
// from the journal, so it survives across processes; it assumes invocations
// of the same command are sequential (the normal shape of an agent test).
func Act(m *manifest.Manifest, logPath string, inv Invocation) (Result, error) {
	spec, ok := m.Commands[inv.Command]
	if !ok {
		return Result{}, fmt.Errorf("no double %q in the manifest (was it rebuilt from a different file?)", inv.Command)
	}

	ruleIdx := -1
	for i, rule := range spec.Rules {
		if match.Args(rule.Match, inv.Args) {
			ruleIdx = i
			break
		}
	}

	prior, err := journal.Read(logPath)
	if err != nil {
		return Result{}, err
	}
	nth := journal.CountRule(prior, inv.Command, ruleIdx) + 1

	resp, matched := resolve(spec, ruleIdx, nth)
	ctx := expand.Context{Command: inv.Command, Args: inv.Args, Nth: nth}
	res := Result{
		Stdout:  expand.Expand(resp.Stdout, ctx),
		Stderr:  expand.Expand(resp.Stderr, ctx),
		Exit:    resp.ExitCode(),
		Rule:    ruleIdx,
		Matched: matched,
	}

	rec := journal.Record{
		Command:        inv.Command,
		Args:           inv.Args,
		Rule:           ruleIdx,
		Matched:        matched,
		Exit:           res.Exit,
		Dir:            inv.Dir,
		Stdin:          inv.Stdin,
		StdinTruncated: inv.StdinTruncated,
	}
	if err := journal.Append(logPath, rec); err != nil {
		return Result{}, err
	}
	return res, nil
}

// resolve picks the response: matched rule (script entry or inline), then
// the command's default, then the built-in diagnostic fallback.
func resolve(spec *manifest.Command, ruleIdx, nth int) (manifest.Response, bool) {
	if ruleIdx >= 0 {
		rule := spec.Rules[ruleIdx]
		if len(rule.Script) > 0 {
			i := nth - 1
			if i >= len(rule.Script) {
				i = len(rule.Script) - 1 // past the end, the last take repeats
			}
			return rule.Script[i], true
		}
		return rule.Response, true
	}
	if spec.Default != nil {
		return *spec.Default, false
	}
	// Built-in fallback. Written with placeholders (expanded by the caller
	// like any other response) so literal braces in argv are not
	// double-expanded.
	exit := 1
	return manifest.Response{
		Exit:   &exit,
		Stderr: "stuntshell: {cmd}: no rule matched: {argv}\n",
	}, false
}
