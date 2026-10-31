"""Updates: find a newer TrueSense release on GitHub and install it.

Checking only reads public release information (one HTTPS request, no identifiers, at most once a day when automatic checks are on). Installing downloads the tagged
release from the project's GitHub repository, checks that it really is that version, and runs its installer with administrator rights (a graphical password prompt, or
sudo in a terminal): the same trust as `curl ... | sudo bash`, but only when you ask. The repository comes from the SYSTEM config (root-owned) or the built-in default,
never from your user config, so a file you can write cannot point the updater somewhere else.
"""
import hashlib, json, os, re, shutil, subprocess, tarfile, tempfile, time, urllib.error, urllib.request
from . import __version__, config
from .device import HwError

CACHE = os.path.expanduser("~/.cache/truesense/update.json")
MAX_BYTES = 40 * 1024 * 1024
REPO_RE = re.compile(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+")


def parse(tag):
    m = re.fullmatch(r"v?(\d+)\.(\d+)\.(\d+)", (tag or "").strip())
    return tuple(int(x) for x in m.groups()) if m else None


def repo():
    """The repository to update from: built-in default, overridable only by the system config."""
    cfg = config.merge(config.copy.deepcopy(config.DEFAULTS), config._read(config.SYSTEM_PATH)) if os.path.exists(config.SYSTEM_PATH) else config.DEFAULTS
    r = cfg["update"]["repo"]
    if not REPO_RE.fullmatch(r): raise HwError(f"[update] repo must look like owner/name, not {r!r}")
    return r


def _get_json(url, timeout=8):
    req = urllib.request.Request(url, headers={"User-Agent": f"truesense/{__version__}", "Accept": "application/vnd.github+json"})
    with urllib.request.urlopen(req, timeout=timeout) as r: return json.load(r)


def latest(r=None):
    """Newest version of the repository: the highest of the newest release and the newest version tag (a tag may not have a release page yet).
    Returns {"version", "tag", "url", "notes"}."""
    r = r or repo(); found = {}
    try:
        d = _get_json(f"https://api.github.com/repos/{r}/releases/latest")
        if parse(d.get("tag_name")): found[d["tag_name"]] = {"url": d.get("html_url", ""), "notes": (d.get("body") or "")[:1500]}
    except urllib.error.HTTPError as e:
        if e.code != 404: raise
    for tg in _get_json(f"https://api.github.com/repos/{r}/tags?per_page=30"):
        if parse(tg.get("name")): found.setdefault(tg["name"], {"url": f"https://github.com/{r}/releases/tag/{tg['name']}", "notes": ""})
    if not found: raise HwError("no release or version tag found in the repository")
    tag = max(found, key=parse)
    return {"version": ".".join(map(str, parse(tag))), "tag": tag, "url": found[tag]["url"], "notes": found[tag]["notes"]}


def _load_cache():
    try:
        with open(CACHE) as f: return json.load(f)
    except (OSError, ValueError): return {}


def _save_cache(d):
    try:
        os.makedirs(os.path.dirname(CACHE), exist_ok=True)
        with open(CACHE + ".tmp", "w") as f: json.dump(d, f)
        os.replace(CACHE + ".tmp", CACHE)
    except OSError: pass


def check(force=False, hours=None):
    """Result: {"current", "latest" (or None), "available", "checked_at", "error", "info"}. Uses the cache unless `force` or it is older than `hours`."""
    hours = hours if hours is not None else config.load()["update"]["check_hours"]; c = _load_cache(); now = time.time()
    if not force and c.get("info") and now - c.get("checked_at", 0) < hours * 3600 and not c.get("error"):
        info = c["info"]
    else:
        try: info = latest(); c = {"checked_at": now, "info": info, "error": None}
        except (urllib.error.URLError, OSError, ValueError, HwError) as e:
            c = {**c, "checked_at": now, "error": f"could not reach GitHub: {getattr(e, 'reason', e)}"}; _save_cache(c)
            info = c.get("info")
            return {"current": __version__, "latest": info["version"] if info else None, "available": bool(info and parse(info["tag"]) > parse(__version__)), "checked_at": now, "error": c["error"], "info": info}
        _save_cache(c)
    return {"current": __version__, "latest": info["version"], "available": parse(info["tag"]) > parse(__version__), "checked_at": c.get("checked_at"), "error": None, "info": info}


# ---- installing ----------------------------------------------------------------------------------------------------------------------
def _safe_extract(tar, dest):
    base = os.path.realpath(dest)
    for m in tar.getmembers():
        target = os.path.realpath(os.path.join(dest, m.name))
        if not (target == base or target.startswith(base + os.sep)): raise HwError(f"the download contains an unsafe path ({m.name}); refusing")
        if m.issym() or m.islnk():
            link = os.path.realpath(os.path.join(os.path.dirname(target), m.linkname))
            if not link.startswith(base + os.sep): raise HwError(f"the download contains an unsafe link ({m.name}); refusing")
        if m.isdev(): raise HwError("the download contains a device file; refusing")
    tar.extractall(dest)


def download(tag, r=None, opener=urllib.request.urlopen):
    """Fetch and unpack a tagged release into a fresh private folder. Returns (folder, sha256). Verifies the version inside matches the tag."""
    r = r or repo(); t = parse(tag)
    if not t: raise HwError(f"not a version tag: {tag!r}")
    work = tempfile.mkdtemp(prefix="truesense-update-"); os.chmod(work, 0o700); url = f"https://github.com/{r}/archive/refs/tags/{tag}.tar.gz"
    try:
        req = urllib.request.Request(url, headers={"User-Agent": f"truesense/{__version__}"}); h = hashlib.sha256(); path = os.path.join(work, "release.tar.gz"); size = 0
        with opener(req, timeout=30) as resp, open(path, "wb") as f:
            while True:
                chunk = resp.read(65536)
                if not chunk: break
                size += len(chunk)
                if size > MAX_BYTES: raise HwError("the download is unexpectedly large; refusing")
                h.update(chunk); f.write(chunk)
        src = os.path.join(work, "src"); os.mkdir(src)
        with tarfile.open(path) as tar: _safe_extract(tar, src)
        roots = os.listdir(src)
        if len(roots) != 1: raise HwError("unexpected layout in the download")
        root = os.path.join(src, roots[0])
        for need in ("tools/install.sh", "src/truesense/__init__.py", "kernel/truesense-wmi/truesense_wmi.c"):
            if not os.path.isfile(os.path.join(root, need)): raise HwError(f"the download is not a TrueSense release (missing {need})")
        with open(os.path.join(root, "src/truesense/__init__.py")) as f: inside = re.search(r'__version__\s*=\s*"([^"]+)"', f.read())
        if not inside or parse(inside.group(1)) != t: raise HwError(f"the download says version {inside.group(1) if inside else '?'}, not {tag}; refusing")
        os.chmod(os.path.join(root, "tools/install.sh"), 0o755); return root, h.hexdigest()
    except BaseException:
        shutil.rmtree(work, ignore_errors=True); raise


def elevate_command(script, gui=False):
    """How to run the installer as root: pkexec (graphical prompt) when there is a desktop and the caller wants it, else sudo; None if we already are root."""
    if os.geteuid() == 0: return [script]
    if gui and shutil.which("pkexec") and (os.environ.get("WAYLAND_DISPLAY") or os.environ.get("DISPLAY")): return ["pkexec", script]
    if shutil.which("sudo"): return ["sudo", script]
    if shutil.which("pkexec"): return ["pkexec", script]
    raise HwError("need sudo or pkexec to install the update")


def apply(tag=None, gui=False, run=subprocess.run, fetch=download):
    """Download the release and run its installer. Returns {"version", "sha256", "returncode"}."""
    info = check(force=True)
    if info["error"] and not info["info"]: raise HwError(info["error"])
    tag = tag or info["info"]["tag"]
    root, sha = fetch(tag)
    try:
        r = run(elevate_command(os.path.join(root, "tools/install.sh"), gui))
        return {"version": ".".join(map(str, parse(tag))), "sha256": sha, "returncode": r.returncode}
    finally: shutil.rmtree(os.path.dirname(os.path.dirname(root)), ignore_errors=True)
