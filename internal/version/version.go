// Package version pins the single source of truth for the release version.
// The go.mod header comment, CHANGELOG.md, and the README badges must all
// agree with this constant; scripts/smoke.sh asserts on it.
package version

// Version is the SemVer release of stuntshell.
const Version = "0.1.0"
