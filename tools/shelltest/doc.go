// Package shelltest holds the tests of the installer scripts (tools/install.sh, tools/get.sh) and
// of the release bundle script. They are shell, not Go, so they are exercised the way a user runs
// them: through bash, with environment variables that point them at fake os-release files, a fake
// DMI directory, fake programs on PATH and a local httptest server standing in for GitHub. Nothing
// here installs anything: install.sh is only ever sourced with TRUESENSE_LIB_ONLY=1 (its functions
// are called one by one) or run with --check inside a fake root; get.sh always runs with
// TRUESENSE_GET_ONLY=1, which verifies and unpacks but never installs.
package shelltest
