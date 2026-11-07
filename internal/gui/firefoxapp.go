package gui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/Order52/truesense/internal/fsx"
)

// Firefox has no "application window" mode, so TrueSense gives it a profile of its own that
// looks like one: no tabs, no address bar, no bookmarks, just the page and the window controls.
// The profile lives under the user's data directory, is separate from the user's browsing
// profile (nothing is shared, nothing of the user's is touched) and is written again when its
// files differ. It also gives the window its own window class ("truesense"), so a desktop can
// group it, give it the TrueSense icon and apply window rules to it.
const firefoxProfileName = "firefox-app"

// firefoxUserJS quiets everything a first run would show: this is an application window, not a browser.
const firefoxUserJS = `// Written by TrueSense. This profile is only for the TrueSense window.
user_pref("toolkit.legacyUserProfileCustomizations.stylesheets", true);
user_pref("browser.shell.checkDefaultBrowser", false);
user_pref("browser.aboutwelcome.enabled", false);
user_pref("browser.startup.homepage_override.mstone", "ignore");
user_pref("startup.homepage_welcome_url", "");
user_pref("startup.homepage_welcome_url.additional", "");
user_pref("browser.tabs.warnOnClose", false);
user_pref("browser.sessionstore.resume_from_crash", false);
user_pref("browser.sessionstore.max_resumed_crashes", 0);
user_pref("browser.toolbars.bookmarks.visibility", "never");
user_pref("browser.newtabpage.enabled", false);
user_pref("browser.disableResetPrompt", true);
user_pref("browser.urlbar.suggest.searches", false);
user_pref("datareporting.policy.dataSubmissionEnabled", false);
user_pref("datareporting.healthreport.uploadEnabled", false);
user_pref("toolkit.telemetry.reportingpolicy.firstRun", false);
user_pref("app.update.auto", false);
`

// firefoxUserChrome hides the browser's own controls. The title bar's window buttons stay.
const firefoxUserChrome = `/* Written by TrueSense: the page is the application, so no tabs, address bar or bookmarks. */
@namespace url("http://www.mozilla.org/keymaster/gatekeeper/there.is.only.xul");
#nav-bar, #PersonalToolbar, #sidebar-main, #sidebar-box { visibility: collapse !important; }
#tabbrowser-tabs, #alltabs-button { visibility: collapse !important; }
`

// writeFirefoxProfile creates or refreshes the profile directory with the two files that make
// it look like an application.
func writeFirefoxProfile(dir string) error {
	files := map[string]string{
		filepath.Join(dir, "user.js"):                  firefoxUserJS,
		filepath.Join(dir, "chrome", "userChrome.css"): firefoxUserChrome,
	}
	for path, content := range files {
		if b, err := os.ReadFile(path); err == nil && string(b) == content {
			continue
		}
		if err := fsx.WriteAtomic(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// firefoxProfileRunning reports whether a Firefox is already running on that profile: its lock
// is a symlink whose target ends in the process id ("127.0.0.1:+1234"). A launch on a profile
// that is running opens a window in that process; a launch on one that is not must start
// its own process (--no-remote), or it would join the user's browsing session instead.
func firefoxProfileRunning(dir string) bool {
	target, err := os.Readlink(filepath.Join(dir, "lock"))
	if err != nil {
		return false
	}
	i := strings.LastIndex(target, "+")
	if i < 0 {
		return false
	}
	pid, err := strconv.Atoi(target[i+1:])
	if err != nil || pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// defaultDataDir is $XDG_DATA_HOME or ~/.local/share.
func defaultDataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share")
	}
	return os.TempDir()
}

// firefoxArgs is the command line that shows url in the TrueSense Firefox profile.
func firefoxArgs(profile, url string, running bool) []string {
	args := []string{"--profile", profile, "--name", WindowClass}
	if !running {
		args = append([]string{"--no-remote"}, args...)
		args = append(args, "-width", "1180", "-height", "820")
	}
	return append(args, "--new-window", url)
}
