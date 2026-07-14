// Package journal persists the invocation log: one JSON object per line,
// appended atomically by each double as it runs. The journal is the ground
// truth that `log`, `verify`, and `assert` all read, and it deliberately
// contains no timestamps so identical test runs produce byte-identical
// journals.
package journal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// Record is one invocation of one double.
type Record struct {
	// Command is the double's name (argv[0] as declared in the manifest).
	Command string `json:"command"`
	// Args is argv after the command name; always non-nil in the journal.
	Args []string `json:"args"`
	// Rule is the zero-based index of the matched rule, or -1 when the
	// invocation fell through to the default response.
	Rule int `json:"rule"`
	// Matched is false when no rule matched (`verify --strict` fails then).
	Matched bool `json:"matched"`
	// Exit is the code the double exited with.
	Exit int `json:"exit"`
	// Dir is the working directory the double ran in.
	Dir string `json:"dir,omitempty"`
	// Stdin holds captured standard input when the command sets
	// capture_stdin, capped at 64 KiB.
	Stdin string `json:"stdin,omitempty"`
	// StdinTruncated reports that stdin exceeded the cap.
	StdinTruncated bool `json:"stdin_truncated,omitempty"`
}

// Argv renders the invocation as a single display line.
func (r Record) Argv() string {
	out := r.Command
	for _, a := range r.Args {
		out += " " + a
	}
	return out
}

// Append writes r as one JSON line. The line is marshaled first and written
// with a single O_APPEND write, so concurrently running doubles can never
// interleave partial records.
func Append(path string, r Record) error {
	if r.Args == nil {
		r.Args = []string{}
	}
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("journal: encode record: %w", err)
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("journal: append: %w", err)
	}
	return nil
}

// Read returns every record in journal order. A missing file is an empty
// journal, not an error — `verify` before any invocation is a legitimate
// (failing) assertion run. Blank lines are ignored.
func Read(path string) ([]Record, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("journal: %w", err)
	}
	var records []Record
	for i, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("journal %s line %d: %w", path, i+1, err)
		}
		records = append(records, r)
	}
	return records, nil
}

// Reset truncates the journal (creating it if absent), starting a fresh
// test case without regenerating the doubles.
func Reset(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("journal: reset: %w", err)
	}
	return f.Close()
}

// CountRule returns how many records already exist for the given command
// and rule index — the basis for script sequencing and the {nth}
// placeholder.
func CountRule(records []Record, command string, rule int) int {
	n := 0
	for _, r := range records {
		if r.Command == command && r.Rule == rule {
			n++
		}
	}
	return n
}
