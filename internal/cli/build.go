// Subcommands that set a stage up: `init` writes a starter manifest,
// `build` generates the doubles, `path` prints the PATH export line.
package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/JaydenCJ/stuntshell/internal/manifest"
	"github.com/JaydenCJ/stuntshell/internal/shim"
)

// DefaultManifest is the conventional manifest file name.
const DefaultManifest = "stuntshell.json"

// DefaultOut is the conventional output directory.
const DefaultOut = ".stunts"

// starterManifest is what `init` writes: a small but real cast showing
// exact matches, per-token globs, the rest token, a retry script, a
// default response, and two expectations. It must always validate —
// TestInitManifestIsValid enforces that.
const starterManifest = `{
  "version": 1,
  "ordered": false,
  "commands": {
    "git": {
      "default": { "exit": 1, "stderr": "git: no stunt rule matched: {argv}\n" },
      "rules": [
        { "match": ["status"], "stdout": "On branch main\nnothing to commit, working tree clean\n" },
        { "match": ["push", "origin", "*"] },
        { "match": ["fetch", "..."], "script": [
          { "exit": 128, "stderr": "fatal: unable to access remote\n" },
          { "exit": 0 }
        ] }
      ]
    },
    "kubectl": {
      "rules": [
        { "match": ["get", "pods", "..."], "stdout": "NAME    READY   STATUS    RESTARTS   AGE\napi-0   1/1     Running   0          9m\n" },
        { "stdout": "kubectl double saw: {argv}\n" }
      ]
    }
  },
  "expect": [
    { "command": "git", "args": ["status"], "min": 1 },
    { "command": "git", "args_glob": ["push", "--force", "..."], "max": 0,
      "description": "never force-push" }
  ]
}
`

func runInit(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("init", stderr)
	force := fs.Bool("force", false, "overwrite an existing manifest")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	path := DefaultManifest
	switch fs.NArg() {
	case 0:
	case 1:
		path = fs.Arg(0)
	default:
		fmt.Fprintf(stderr, "stuntshell init: expected at most one file argument, got %d\n", fs.NArg())
		return ExitUsage
	}
	if !*force {
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintf(stderr, "stuntshell init: %s already exists (use --force to overwrite)\n", path)
			return ExitRuntime
		}
	}
	if err := os.WriteFile(path, []byte(starterManifest), 0o644); err != nil {
		return runtimeErr(stderr, err)
	}
	fmt.Fprintf(stdout, "wrote %s — edit the cast, then run: stuntshell build --manifest %s\n", path, path)
	return ExitOK
}

func runBuild(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("build", stderr)
	manifestPath := fs.String("manifest", DefaultManifest, "manifest file to load")
	out := fs.String("out", DefaultOut, "output directory for doubles and journal")
	logPath := fs.String("log", "", "journal path (default <out>/invocations.jsonl)")
	bin := fs.String("bin", "", "stuntshell binary the doubles exec (default: this executable)")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if !noPositional(fs, stderr) {
		return ExitUsage
	}
	m, err := manifest.Load(*manifestPath)
	if err != nil {
		return runtimeErr(stderr, err)
	}
	binPath := *bin
	if binPath == "" {
		if binPath, err = os.Executable(); err != nil {
			return runtimeErr(stderr, fmt.Errorf("locate stuntshell binary (pass --bin): %w", err))
		}
	}
	res, err := shim.Build(m, *out, binPath, *manifestPath, *logPath)
	if err != nil {
		return runtimeErr(stderr, err)
	}
	fmt.Fprintf(stdout, "stuntshell build — %d %s staged\n", len(res.Commands), plural(len(res.Commands), "double", "doubles"))
	fmt.Fprintf(stdout, "  bin  %s\n", res.BinDir)
	fmt.Fprintf(stdout, "  log  %s\n", res.LogPath)
	fmt.Fprintf(stdout, "  %s\n\n", strings.Join(res.Commands, ", "))
	fmt.Fprintln(stdout, shim.ExportLine(res.BinDir))
	return ExitOK
}

func runPath(args []string, stdout, stderr io.Writer) int {
	fset := newFlagSet("path", stderr)
	out := fset.String("out", DefaultOut, "output directory used at build time")
	if err := fset.Parse(args); err != nil {
		return ExitUsage
	}
	if !noPositional(fset, stderr) {
		return ExitUsage
	}
	binDir, err := filepath.Abs(filepath.Join(*out, "bin"))
	if err != nil {
		return runtimeErr(stderr, err)
	}
	if _, err := os.Stat(binDir); errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(stderr, "stuntshell path: %s does not exist — run `stuntshell build` first\n", binDir)
		return ExitRuntime
	}
	fmt.Fprintln(stdout, shim.ExportLine(binDir))
	return ExitOK
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
