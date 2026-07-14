// `__act` is the internal entry point every generated double execs into.
// It is not part of the public CLI surface, but its behavior IS the
// double's behavior, so it is exercised heavily by tests and the smoke
// script.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/JaydenCJ/stuntshell/internal/actor"
	"github.com/JaydenCJ/stuntshell/internal/manifest"
)

// stdinCap bounds how much captured stdin lands in the journal. 64 KiB is
// plenty for asserting on piped configs while keeping journals reviewable.
const stdinCap = 64 * 1024

func runAct(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := newFlagSet("__act", stderr)
	manifestPath := fs.String("manifest", "", "manifest file (baked in by build)")
	logPath := fs.String("log", "", "journal file (baked in by build)")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	rest := fs.Args()
	if *manifestPath == "" || *logPath == "" || len(rest) == 0 {
		fmt.Fprintln(stderr, "stuntshell __act: need --manifest, --log, and -- <command> [args…]")
		return ExitUsage
	}
	m, err := manifest.Load(*manifestPath)
	if err != nil {
		return runtimeErr(stderr, err)
	}

	inv := actor.Invocation{Command: rest[0], Args: rest[1:]}
	// The working directory is evidence (which repo did the agent push
	// from?), but its absence must not break the double.
	if dir, err := os.Getwd(); err == nil {
		inv.Dir = dir
	}
	if spec, ok := m.Commands[inv.Command]; ok && spec.CaptureStdin {
		data, truncated, err := readCapped(stdin, stdinCap)
		if err != nil {
			return runtimeErr(stderr, fmt.Errorf("read stdin: %w", err))
		}
		inv.Stdin, inv.StdinTruncated = string(data), truncated
	}

	res, err := actor.Act(m, *logPath, inv)
	if err != nil {
		return runtimeErr(stderr, err)
	}
	if _, err := io.WriteString(stdout, res.Stdout); err != nil {
		return ExitRuntime
	}
	if _, err := io.WriteString(stderr, res.Stderr); err != nil {
		return ExitRuntime
	}
	return res.Exit
}

// readCapped reads at most cap bytes and reports whether more were
// available (by attempting one extra byte).
func readCapped(r io.Reader, capBytes int) ([]byte, bool, error) {
	if r == nil {
		return nil, false, nil
	}
	data, err := io.ReadAll(io.LimitReader(r, int64(capBytes)+1))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	if len(data) > capBytes {
		return data[:capBytes], true, nil
	}
	return data, false, nil
}
