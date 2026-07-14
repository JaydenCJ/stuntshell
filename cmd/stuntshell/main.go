// Command stuntshell generates fake command-line executables (stunt
// doubles) from a manifest, records every invocation, and asserts on the
// result — deterministic stand-ins for git, kubectl, and friends when
// testing agents that run shell commands.
package main

import (
	"os"

	"github.com/JaydenCJ/stuntshell/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
