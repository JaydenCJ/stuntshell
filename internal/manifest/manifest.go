// Package manifest defines the stuntshell manifest — the declarative
// description of every stunt double, its response rules, and the
// expectations the invocation journal must satisfy — plus strict loading
// and validation. The full format reference lives in docs/manifest.md.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/JaydenCJ/stuntshell/internal/match"
)

// CurrentVersion is the only manifest schema version this build understands.
const CurrentVersion = 1

// Manifest is the root document (conventionally stuntshell.json).
type Manifest struct {
	// Version must equal CurrentVersion.
	Version int `json:"version"`
	// Strict makes `verify` fail when any invocation hit no rule, even
	// without --strict on the command line.
	Strict bool `json:"strict,omitempty"`
	// Ordered requires the first match of each expectation (with an
	// effective minimum ≥ 1) to appear in the journal in listed order.
	Ordered bool `json:"ordered,omitempty"`
	// Commands maps each double's executable name to its behavior.
	Commands map[string]*Command `json:"commands"`
	// Expect lists the invocation assertions `verify` evaluates.
	Expect []Expectation `json:"expect,omitempty"`
}

// Command describes one stunt double.
type Command struct {
	// CaptureStdin records up to 64 KiB of the double's stdin in the
	// journal (off by default: doubles that never read stdin behave like
	// real commands that ignore it).
	CaptureStdin bool `json:"capture_stdin,omitempty"`
	// Default is the response when no rule matches. When nil, stuntshell
	// answers with exit 1 and a diagnostic on stderr.
	Default *Response `json:"default,omitempty"`
	// Rules are tried in order; the first match wins.
	Rules []Rule `json:"rules,omitempty"`
}

// Rule pairs an argv pattern with either one inline response or a script
// of consecutive responses.
type Rule struct {
	// Match is the argv pattern: per-token globs (`*`, `?`, `\`-escapes)
	// with "..." allowed in final position to consume the remainder.
	// Omitted (null) matches ANY argv; [] matches only an empty argv.
	Match []string `json:"match,omitempty"`
	// Response is the inline reply (mutually exclusive with Script).
	Response
	// Script replies with its nth entry on the nth match of this rule;
	// after the last entry, the last entry repeats. Perfect for "fail
	// twice, then succeed" retry scenarios.
	Script []Response `json:"script,omitempty"`
}

// Response is what a double prints and how it exits.
type Response struct {
	// Stdout and Stderr may use placeholders: {cmd} {argv} {0}… {#} {nth}.
	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`
	// Exit is the process exit code; nil means 0.
	Exit *int `json:"exit,omitempty"`
}

// ExitCode resolves the exit code, defaulting to 0.
func (r Response) ExitCode() int {
	if r.Exit == nil {
		return 0
	}
	return *r.Exit
}

// isZero reports whether no inline response field was set, which is how
// validation detects an inline-response/script conflict.
func (r Response) isZero() bool {
	return r.Stdout == "" && r.Stderr == "" && r.Exit == nil
}

// Expectation is one invocation assertion evaluated by `verify`.
type Expectation struct {
	// Description labels the expectation in reports (optional).
	Description string `json:"description,omitempty"`
	// Command names the double this expectation counts (required, and it
	// must exist under "commands" so typos fail at load time).
	Command string `json:"command"`
	// Args matches argv exactly, token for token. Omitted means any argv;
	// [] means an empty argv. Mutually exclusive with ArgsGlob.
	Args []string `json:"args,omitempty"`
	// ArgsGlob matches argv with the same pattern language as rules.
	ArgsGlob []string `json:"args_glob,omitempty"`
	// Min / Max / Exactly bound the matching-invocation count. With none
	// set the expectation means "called at least once" (min 1); Exactly
	// excludes the other two.
	Min     *int `json:"min,omitempty"`
	Max     *int `json:"max,omitempty"`
	Exactly *int `json:"exactly,omitempty"`
}

// Bounds returns the effective inclusive count bounds. bounded reports
// whether an upper bound applies at all.
func (e Expectation) Bounds() (min, max int, bounded bool) {
	if e.Exactly != nil {
		return *e.Exactly, *e.Exactly, true
	}
	switch {
	case e.Min != nil:
		min = *e.Min
	case e.Max == nil:
		min = 1 // bare expectation: "must have been called"
	}
	if e.Max != nil {
		return min, *e.Max, true
	}
	return min, 0, false
}

// Label returns a stable one-line identity for reports: the description if
// present, otherwise the command followed by its argv pattern.
func (e Expectation) Label() string {
	if e.Description != "" {
		return e.Description
	}
	parts := []string{e.Command}
	switch {
	case e.Args != nil:
		parts = append(parts, e.Args...)
	case e.ArgsGlob != nil:
		parts = append(parts, e.ArgsGlob...)
	default:
		parts = append(parts, "…")
	}
	return strings.Join(parts, " ")
}

// Load reads and validates a manifest file.
func Load(path string) (*Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	defer f.Close()
	m, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", path, err)
	}
	return m, nil
}

// Parse decodes and validates a manifest. Unknown JSON keys are rejected so
// a typo like "stdotu" fails loudly instead of silently doing nothing —
// exactly the failure mode a testing tool must not have.
func Parse(r io.Reader) (*Manifest, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if dec.More() {
		return nil, errors.New("parse: trailing data after the top-level object")
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks structural invariants. Errors name the offending command
// or expectation so a broken manifest is a one-edit fix.
func (m *Manifest) Validate() error {
	if m.Version != CurrentVersion {
		return fmt.Errorf("unsupported manifest version %d (this build understands %d)", m.Version, CurrentVersion)
	}
	if len(m.Commands) == 0 {
		return errors.New(`at least one command is required under "commands"`)
	}
	for _, name := range m.CommandNames() {
		if err := validateName(name); err != nil {
			return err
		}
		cmd := m.Commands[name]
		if cmd == nil {
			return fmt.Errorf("command %q: definition is null", name)
		}
		for i, rule := range cmd.Rules {
			if err := validatePattern(rule.Match); err != nil {
				return fmt.Errorf("command %q rule %d: %w", name, i, err)
			}
			if len(rule.Script) > 0 && !rule.Response.isZero() {
				return fmt.Errorf("command %q rule %d: set either an inline response or a script, not both", name, i)
			}
		}
	}
	for i, e := range m.Expect {
		if err := m.validateExpectation(e); err != nil {
			return fmt.Errorf("expect[%d]: %w", i, err)
		}
	}
	return nil
}

func (m *Manifest) validateExpectation(e Expectation) error {
	if e.Command == "" {
		return errors.New(`"command" is required`)
	}
	if _, ok := m.Commands[e.Command]; !ok {
		return fmt.Errorf("unknown command %q (expectations may only reference declared doubles)", e.Command)
	}
	if e.Args != nil && e.ArgsGlob != nil {
		return errors.New(`set either "args" or "args_glob", not both`)
	}
	if err := validatePattern(e.ArgsGlob); err != nil {
		return err
	}
	if e.Exactly != nil && (e.Min != nil || e.Max != nil) {
		return errors.New(`"exactly" excludes "min" and "max"`)
	}
	for _, bound := range []struct {
		name string
		v    *int
	}{{"min", e.Min}, {"max", e.Max}, {"exactly", e.Exactly}} {
		if bound.v != nil && *bound.v < 0 {
			return fmt.Errorf("%q must be ≥ 0, got %d", bound.name, *bound.v)
		}
	}
	if e.Min != nil && e.Max != nil && *e.Min > *e.Max {
		return fmt.Errorf(`"min" %d exceeds "max" %d`, *e.Min, *e.Max)
	}
	return nil
}

// validatePattern rejects a rest token anywhere but the final position;
// "git ... push" would otherwise silently never match.
func validatePattern(tokens []string) error {
	for i, tok := range tokens {
		if tok == match.Rest && i != len(tokens)-1 {
			return fmt.Errorf("%q is only allowed as the final pattern token", match.Rest)
		}
	}
	return nil
}

// validateName ensures the command name is a safe, standalone file name —
// it becomes an executable inside the generated bin directory.
func validateName(name string) error {
	if name == "" {
		return errors.New("command name must not be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("command name %q is not a valid file name", name)
	}
	if name == "stuntshell" {
		return errors.New(`command name "stuntshell" would shadow stuntshell itself`)
	}
	for _, r := range name {
		if r == '/' || r == '\\' || r == 0 {
			return fmt.Errorf("command name %q must not contain path separators", name)
		}
		if r <= 0x1f || r == ' ' {
			return fmt.Errorf("command name %q must not contain whitespace or control characters", name)
		}
	}
	return nil
}

// CommandNames returns the declared double names in sorted order, so shim
// generation and error reporting are deterministic.
func (m *Manifest) CommandNames() []string {
	names := make([]string, 0, len(m.Commands))
	for name := range m.Commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
