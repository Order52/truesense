package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Order52/truesense/internal/buildinfo"
	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/fsx"
)

// Production endpoints. They are fields of Updater (not user settings) so tests can point
// them at an httptest server.
const (
	DefaultAPIBase = "https://api.github.com"
	DefaultWebBase = "https://github.com"
)

// DefaultMaxDownload caps a release download: the real archive is a few megabytes.
const DefaultMaxDownload = 40 << 20

var repoRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// ValidRepo reports whether r looks like "owner/name". The repository ends up in URLs, so
// nothing else (no "..", no query) may pass.
func ValidRepo(r string) bool {
	owner, name, ok := strings.Cut(r, "/")
	if !ok || !repoRE.MatchString(r) {
		return false
	}
	return owner != "." && owner != ".." && name != "." && name != ".." // "../.." fits the pattern but climbs out of /repos/
}

// SystemRepo is the repository to update from: the built-in default, overridable only by the
// system config (root-owned). The user's config is deliberately not read.
func SystemRepo(systemConfigPath string) (string, error) {
	cfg, err := config.Load(systemConfigPath, "")
	if err != nil {
		return "", fmt.Errorf("read the system config: %w", err)
	}
	if !ValidRepo(cfg.Update.Repo) {
		return "", fmt.Errorf("[update] repo must look like owner/name, not %q", cfg.Update.Repo)
	}
	return cfg.Update.Repo, nil
}

// Info is the newest release found. The JSON names are the 0.3.x ones.
type Info struct {
	Version string `json:"version"`
	Tag     string `json:"tag"`
	URL     string `json:"url"`
	Notes   string `json:"notes"`
}

// Updater finds and installs releases. Build one with New and override fields in tests.
type Updater struct {
	Repo       string
	APIBase    string
	WebBase    string
	HTTP       *http.Client
	CachePath  string
	Current    string // the running version
	CheckHours int    // how long a cached answer stays fresh
	Now        func() time.Time
	TempDir    string // where downloads are unpacked; "" = the system temporary directory
	Layout     Layout
	Host       Host
	// MaxDownload is the most bytes an archive may have; a larger download is refused.
	MaxDownload int64
}

// New returns an Updater for repo with production endpoints, the standard cache file and the
// running version.
func New(repo string) *Updater {
	cache := ""
	if dir, err := os.UserCacheDir(); err == nil {
		cache = filepath.Join(dir, "truesense", "update.json")
	}
	return &Updater{
		Repo: repo, APIBase: DefaultAPIBase, WebBase: DefaultWebBase, HTTP: &http.Client{}, CachePath: cache,
		Current: buildinfo.Version, CheckHours: 24, Now: time.Now, Layout: DefaultLayout(), Host: DefaultHost(), MaxDownload: DefaultMaxDownload,
	}
}

// HTTPError is a GitHub answer that was not a success.
type HTTPError struct {
	Code int
	URL  string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("HTTP %d from %s", e.Code, e.URL) }

func (u *Updater) userAgent() string { return "truesense/" + u.Current }

func (u *Updater) get(ctx context.Context, url, accept string, timeout time.Duration) (*http.Response, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	req.Header.Set("User-Agent", u.userAgent())
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := u.HTTP.Do(req)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return resp, cancel, nil
}

func (u *Updater) getJSON(ctx context.Context, url string, out any) error {
	resp, cancel, err := u.get(ctx, url, "application/vnd.github+json", 8*time.Second)
	if err != nil {
		return err
	}
	defer cancel()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return &HTTPError{Code: resp.StatusCode, URL: url}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// Latest is the newest version of the repository: the highest of the newest release and the
// newest version tag (a tag may not have a release page yet).
func (u *Updater) Latest(ctx context.Context) (Info, error) {
	type found struct {
		tag string
		v   Version
		Info
	}
	var all []found
	add := func(tag, url, notes string) {
		v, ok := ParseVersion(tag)
		if !ok {
			return
		}
		for i := range all {
			if all[i].tag == tag { // the release page (added first) wins over the bare tag
				return
			}
		}
		all = append(all, found{tag: tag, v: v, Info: Info{Version: v.String(), Tag: tag, URL: url, Notes: notes}})
	}
	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
	}
	err := u.getJSON(ctx, fmt.Sprintf("%s/repos/%s/releases/latest", u.APIBase, u.Repo), &rel)
	var httpErr *HTTPError
	switch {
	case err == nil:
		add(rel.TagName, rel.HTMLURL, truncateRunes(rel.Body, 1500))
	case errors.As(err, &httpErr) && httpErr.Code == http.StatusNotFound:
	default:
		return Info{}, err
	}
	var tags []struct {
		Name string `json:"name"`
	}
	if err := u.getJSON(ctx, fmt.Sprintf("%s/repos/%s/tags?per_page=30", u.APIBase, u.Repo), &tags); err != nil {
		return Info{}, err
	}
	for _, t := range tags {
		add(t.Name, fmt.Sprintf("%s/%s/releases/tag/%s", u.WebBase, u.Repo, t.Name), "")
	}
	if len(all) == 0 {
		return Info{}, errors.New("no release or version tag found in the repository")
	}
	best := all[0]
	for _, f := range all[1:] {
		if f.v.Compare(best.v) > 0 {
			best = f
		}
	}
	return best.Info, nil
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// cache is the file that remembers the last answer: {"checked_at", "info", "error"}.
type cache struct {
	CheckedAt float64 `json:"checked_at"`
	Info      *Info   `json:"info"`
	Error     *string `json:"error"`
}

func (u *Updater) loadCache() cache {
	var c cache
	if b, err := os.ReadFile(u.CachePath); err == nil && json.Unmarshal(b, &c) != nil {
		return cache{}
	}
	return c
}

func (u *Updater) saveCache(c cache) {
	if b, err := json.Marshal(c); err == nil && u.CachePath != "" {
		_ = fsx.WriteAtomic(u.CachePath, b, 0o644) // a convenience file: a read-only home is not an error
	}
}

// Status is the answer of Check. The JSON names are the 0.3.x ones.
type Status struct {
	Current   string  `json:"current"`
	Latest    *string `json:"latest"`
	Available bool    `json:"available"`
	CheckedAt float64 `json:"checked_at"`
	Error     *string `json:"error"`
	Info      *Info   `json:"info"`
}

func unixSeconds(t time.Time) float64 { return float64(t.UnixNano()) / 1e9 }

func (u *Updater) status(info *Info, checkedAt float64, errText *string) Status {
	s := Status{Current: u.Current, CheckedAt: checkedAt, Error: errText, Info: info}
	if info != nil {
		s.Latest = &info.Version
		cur, okCur := ParseVersion(u.Current)
		tag, okTag := ParseVersion(info.Tag)
		s.Available = okCur && okTag && tag.Compare(cur) > 0
	}
	return s
}

// Check says whether a newer release exists. It uses the cached answer unless force is set or
// the cache is older than CheckHours (hours <= 0 means the Updater's setting), and a failed
// lookup is reported in Status.Error together with whatever was cached, never as an error.
func (u *Updater) Check(ctx context.Context, force bool, hours int) Status {
	if hours <= 0 {
		hours = u.CheckHours
	}
	c, now := u.loadCache(), u.Now()
	t := unixSeconds(now)
	if !force && c.Info != nil && t-c.CheckedAt < float64(hours)*3600 && c.Error == nil {
		return u.status(c.Info, c.CheckedAt, nil)
	}
	info, err := u.Latest(ctx)
	if err != nil {
		msg := "could not reach GitHub: " + err.Error()
		c.CheckedAt, c.Error = t, &msg
		u.saveCache(c)
		return u.status(c.Info, t, &msg)
	}
	c = cache{CheckedAt: t, Info: &info}
	u.saveCache(c)
	return u.status(&info, t, nil)
}
