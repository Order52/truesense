// Package buildinfo holds the version of this build. Version is the release number
// (tools/release.py keeps it in step with the kernel module and the packages);
// Commit and Date are filled in by the release build with -ldflags -X.
package buildinfo

// Version is the TrueSense release this source tree is.
const Version = "0.3.17"

// Commit is the git commit of the build ("" for a plain `go build`).
var Commit = ""

// Date is the build date in UTC ("" for a plain `go build`).
var Date = ""
