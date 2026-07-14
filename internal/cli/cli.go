// Package cli implements the stuntshell command-line interface. Run takes
// argv plus the three standard streams and returns an exit code, so the
// whole surface is testable in-process without building a binary.
package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/JaydenCJ/stuntshell/internal/version"
)

// Exit codes, documented in the README. `__act` is the exception: it exits
// with whatever code the matched response declares — that IS the double's
// contract with the program under test.
const (
	ExitOK      = 0
	ExitFail    = 1 // verify/assert failed
	ExitUsage   = 2
	ExitRuntime = 3
)

// Run dispatches argv and returns the process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stdout)
		return ExitOK
	}
	switch args[0] {
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "build":
		return runBuild(args[1:], stdout, stderr)
	case "path":
		return runPath(args[1:], stdout, stderr)
	case "log":
		return runLog(args[1:], stdout, stderr)
	case "verify":
		return runVerify(args[1:], stdout, stderr)
	case "assert":
		return runAssert(args[1:], stdout, stderr)
	case "reset":
		return runReset(args[1:], stdout, stderr)
	case "__act":
		return runAct(args[1:], stdin, stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "stuntshell %s\n", version.Version)
		return ExitOK
	case "help", "--help", "-h":
		usage(stdout)
		return ExitOK
	default:
		fmt.Fprintf(stderr, "stuntshell: unknown command %q\n\n", args[0])
		usage(stderr)
		return ExitUsage
	}
}

// newFlagSet builds a flag set that reports parse errors to stderr and
// never calls os.Exit.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// noPositional enforces that a subcommand received no stray arguments.
func noPositional(fs *flag.FlagSet, stderr io.Writer) bool {
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "stuntshell %s: unexpected argument %q\n", fs.Name(), fs.Arg(0))
		return false
	}
	return true
}

func runtimeErr(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "stuntshell: %v\n", err)
	return ExitRuntime
}

// splitFields turns a flag like --args "push origin main" into tokens.
// Splitting is on whitespace; arguments that themselves contain spaces
// need a manifest expectation instead (documented limitation).
func splitFields(s string) []string {
	f := strings.Fields(s)
	if f == nil {
		return []string{}
	}
	return f
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `stuntshell %s — manifest-driven stunt doubles for shell commands

Usage:
  stuntshell init [--force] [file]        write a starter manifest (default stuntshell.json)
  stuntshell build [flags]                generate doubles into --out from --manifest
  stuntshell path [--out DIR]             print the PATH export line for eval
  stuntshell log [flags]                  list recorded invocations
  stuntshell verify [flags]               evaluate the manifest's expectations (exit 1 on failure)
  stuntshell assert [flags]               evaluate one ad-hoc expectation (exit 1 on failure)
  stuntshell reset [--log FILE]           truncate the journal for a fresh test case
  stuntshell version                      print the version

Build flags:
  --manifest FILE   manifest to load (default stuntshell.json)
  --out DIR         output directory (default .stunts); doubles go to DIR/bin
  --log FILE        journal path (default DIR/invocations.jsonl)
  --bin FILE        stuntshell binary the doubles exec (default: this executable)

Log flags:      --log FILE · --format text|json · --command NAME
Verify flags:   --manifest FILE · --log FILE · --strict · --format text|json
Assert flags:   --log FILE · --command NAME · --args "TOKENS" | --args-glob "TOKENS"
                --min N · --max N · --exactly N

Exit codes: 0 ok · 1 expectation failed · 2 usage error · 3 runtime error
(__act is internal: generated doubles exec it, and it exits with the
matched response's declared exit code.)
`, version.Version)
}
