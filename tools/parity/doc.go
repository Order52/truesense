// Package parity runs the same commands with the 0.3.x Python command line and with the Go
// one, both against the firmware simulator and private state, and compares their --json
// output structurally. It is a test-only package: `go test ./tools/parity/` skips itself when
// python3 or the Python tree (src/truesense) is missing, and it never touches the real
// machine (the environment is TRUESENSE_SIM=1, a socket that does not exist, a private home
// and user state, and a PATH with no programs in it, so pactl and systemctl cannot be reached).
package parity
