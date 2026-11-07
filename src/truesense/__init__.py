# SPDX-License-Identifier: MIT
# Compatibility shim, not code. TrueSense is a Go program (see cmd/truesense and internal/). The
# in-app updater of 0.3.x installs, which is frozen Python, reads the version of a downloaded
# release from this file and refuses a release that does not contain it. Keep it, and keep it in
# step with internal/buildinfo (cmd/release does that); it can go once 0.3.x installs are gone.
__version__ = "0.4.0"
