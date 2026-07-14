// Subcommands that read the journal back: `log` lists invocations,
// `verify` evaluates the manifest's expectations, `assert` evaluates one
// ad-hoc expectation, and `reset` truncates the journal.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/JaydenCJ/stuntshell/internal/journal"
	"github.com/JaydenCJ/stuntshell/internal/manifest"
	"github.com/JaydenCJ/stuntshell/internal/verify"
)

// defaultLog mirrors the build default so the read-side commands work
// without flags in the conventional layout.
const defaultLog = DefaultOut + "/invocations.jsonl"

func runLog(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("log", stderr)
	logPath := fs.String("log", defaultLog, "journal file to read")
	format := fs.String("format", "text", "output format: text or json")
	command := fs.String("command", "", "only show invocations of this double")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if !noPositional(fs, stderr) {
		return ExitUsage
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "stuntshell log: unknown --format %q (want text or json)\n", *format)
		return ExitUsage
	}
	records, err := journal.Read(*logPath)
	if err != nil {
		return runtimeErr(stderr, err)
	}
	if *command != "" {
		filtered := records[:0]
		for _, r := range records {
			if r.Command == *command {
				filtered = append(filtered, r)
			}
		}
		records = filtered
	}
	if *format == "json" {
		return writeJSON(stdout, stderr, records)
	}
	fmt.Fprintf(stdout, "%d %s\n", len(records), plural(len(records), "invocation", "invocations"))
	for i, r := range records {
		where := fmt.Sprintf("rule %d", r.Rule)
		if !r.Matched {
			where = "(no rule)"
		}
		fmt.Fprintf(stdout, "  %3d  %-40s %-9s exit %d\n", i+1, r.Argv(), where, r.Exit)
	}
	return ExitOK
}

func runVerify(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("verify", stderr)
	manifestPath := fs.String("manifest", DefaultManifest, "manifest with the expect list")
	logPath := fs.String("log", defaultLog, "journal file to judge")
	strict := fs.Bool("strict", false, "also fail on invocations that hit no rule")
	format := fs.String("format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if !noPositional(fs, stderr) {
		return ExitUsage
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "stuntshell verify: unknown --format %q (want text or json)\n", *format)
		return ExitUsage
	}
	m, err := manifest.Load(*manifestPath)
	if err != nil {
		return runtimeErr(stderr, err)
	}
	records, err := journal.Read(*logPath)
	if err != nil {
		return runtimeErr(stderr, err)
	}
	rep := verify.Run(m, records, *strict)
	if *format == "json" {
		if code := writeJSON(stdout, stderr, rep); code != ExitOK {
			return code
		}
		return passCode(rep.Pass)
	}
	fmt.Fprintf(stdout, "stuntshell verify — %d %s, %d %s\n\n",
		len(rep.Outcomes), plural(len(rep.Outcomes), "expectation", "expectations"),
		rep.Invocations, plural(rep.Invocations, "invocation", "invocations"))
	for _, out := range rep.Outcomes {
		verdict := "ok  "
		if !out.OK {
			verdict = "FAIL"
		}
		fmt.Fprintf(stdout, "  %s  %-38s called %d× (%s)\n", verdict, out.Label, out.Count, out.Want)
	}
	if rep.Strict {
		if len(rep.Unmatched) > 0 {
			fmt.Fprintf(stdout, "\nstrict: FAIL — %d %s hit no rule (see `stuntshell log`)\n",
				len(rep.Unmatched), plural(len(rep.Unmatched), "invocation", "invocations"))
		} else {
			fmt.Fprintf(stdout, "\nstrict: ok — every invocation matched a rule\n")
		}
	}
	if rep.OrderViolation != "" {
		fmt.Fprintf(stdout, "\norder: FAIL — %s\n", rep.OrderViolation)
	}
	fmt.Fprintf(stdout, "\nverify: %s\n", passWord(rep.Pass))
	return passCode(rep.Pass)
}

func runAssert(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("assert", stderr)
	logPath := fs.String("log", defaultLog, "journal file to judge")
	command := fs.String("command", "", "the double whose invocations are counted (required)")
	argsExact := fs.String("args", "", "exact argv, whitespace-separated tokens")
	argsGlob := fs.String("args-glob", "", "argv glob pattern, whitespace-separated tokens")
	min := fs.Int("min", -1, "minimum matching invocations")
	max := fs.Int("max", -1, "maximum matching invocations")
	exactly := fs.Int("exactly", -1, "exact matching-invocation count")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if !noPositional(fs, stderr) {
		return ExitUsage
	}
	e, err := buildExpectation(*command, *argsExact, *argsGlob, *min, *max, *exactly)
	if err != nil {
		fmt.Fprintf(stderr, "stuntshell assert: %v\n", err)
		return ExitUsage
	}
	records, err := journal.Read(*logPath)
	if err != nil {
		return runtimeErr(stderr, err)
	}
	out := verify.Eval(e, records)
	fmt.Fprintf(stdout, "assert %s: %s — called %d× (%s)\n", out.Label, passWord(out.OK), out.Count, out.Want)
	return passCode(out.OK)
}

// buildExpectation lifts assert's flags into a manifest.Expectation and
// applies the same structural checks the manifest loader would.
func buildExpectation(command, argsExact, argsGlob string, min, max, exactly int) (manifest.Expectation, error) {
	var e manifest.Expectation
	if command == "" {
		return e, fmt.Errorf("--command is required")
	}
	e.Command = command
	if argsExact != "" && argsGlob != "" {
		return e, fmt.Errorf("set either --args or --args-glob, not both")
	}
	if argsExact != "" {
		e.Args = splitFields(argsExact)
	}
	if argsGlob != "" {
		e.ArgsGlob = splitFields(argsGlob)
	}
	if exactly >= 0 {
		if min >= 0 || max >= 0 {
			return e, fmt.Errorf("--exactly excludes --min and --max")
		}
		e.Exactly = &exactly
	}
	if min >= 0 {
		e.Min = &min
	}
	if max >= 0 {
		e.Max = &max
	}
	if min >= 0 && max >= 0 && min > max {
		return e, fmt.Errorf("--min %d exceeds --max %d", min, max)
	}
	for i, tok := range e.ArgsGlob {
		if tok == "..." && i != len(e.ArgsGlob)-1 {
			return e, fmt.Errorf(`"..." is only allowed as the final pattern token`)
		}
	}
	return e, nil
}

func runReset(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("reset", stderr)
	logPath := fs.String("log", defaultLog, "journal file to truncate")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if !noPositional(fs, stderr) {
		return ExitUsage
	}
	if err := journal.Reset(*logPath); err != nil {
		return runtimeErr(stderr, err)
	}
	fmt.Fprintf(stdout, "journal reset: %s\n", *logPath)
	return ExitOK
}

func writeJSON(stdout, stderr io.Writer, v any) int {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return runtimeErr(stderr, err)
	}
	fmt.Fprintln(stdout, strings.TrimRight(string(data), "\n"))
	return ExitOK
}

func passWord(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

func passCode(ok bool) int {
	if ok {
		return ExitOK
	}
	return ExitFail
}
