# SPDX-License-Identifier: MIT
"""tools/get.sh (the one-line installer) against a local fake GitHub: it must verify what it downloads and refuse when it cannot."""
import hashlib
import http.server
import io
import os
import subprocess
import tarfile
import threading
import unittest

ROOT = os.path.realpath(os.path.join(os.path.dirname(__file__), ".."))
GET = os.path.join(ROOT, "tools", "get.sh")


def tarball(version, inside=None):
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as tf:
        for name, data in {"tools/install.sh": b"#!/bin/sh\n", "src/truesense/__init__.py": f'__version__ = "{inside or version}"\n'.encode()}.items():
            ti = tarfile.TarInfo(f"truesense-{version}/{name}"); ti.size = len(data); tf.addfile(ti, io.BytesIO(data))
    return buf.getvalue()


class FakeGitHub:
    def __init__(self, files):
        files_ = files
        class H(http.server.BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"                                       # explicit, with a length and a closed connection on every answer (what GitHub's CDN does)

            def reply(self, code, body=b""):
                self.send_response(code); self.send_header("Content-Length", str(len(body))); self.send_header("Connection", "close"); self.end_headers(); self.wfile.write(body); self.close_connection = True

            def do_GET(self):
                body = files_.get(self.path.split("?")[0])
                if isinstance(body, int): self.reply(body)
                elif body is None: self.reply(404)
                else: self.reply(200, body)
            def log_message(self, *a): pass
        self.srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H); self.url = f"http://127.0.0.1:{self.srv.server_port}"
        threading.Thread(target=self.srv.serve_forever, daemon=True).start()

    def close(self): self.srv.shutdown(); self.srv.server_close()


def release(version, sums="auto", asset=True, inside=None, archive=False):
    """The files a release of `version` has on the fake GitHub."""
    blob = tarball(version, inside); base = f"/Order52/truesense/releases/download/v{version}"; f = {}
    if asset: f[f"{base}/truesense-{version}.tar.gz"] = blob
    if sums == "auto": f[f"{base}/SHA256SUMS"] = f"{hashlib.sha256(blob).hexdigest()}  truesense-{version}.tar.gz\n".encode()
    elif isinstance(sums, (str, bytes)): f[f"{base}/SHA256SUMS"] = sums.encode() if isinstance(sums, str) else sums
    elif isinstance(sums, int): f[f"{base}/SHA256SUMS"] = sums
    if archive: f[f"/Order52/truesense/archive/refs/tags/v{version}.tar.gz"] = blob
    return f


class TestGetScript(unittest.TestCase):
    def run_get(self, files, version="0.3.16", extra_env=None):
        gh = FakeGitHub(files)
        try:
            env = {**os.environ, "TRUESENSE_WEB": gh.url, "TRUESENSE_API": gh.url, "TRUESENSE_GET_ONLY": "1", "TRUESENSE_VERSION": version}; env.update(extra_env or {})
            if version is None: env.pop("TRUESENSE_VERSION")
            return subprocess.run(["bash", GET], capture_output=True, text=True, env=env, timeout=60)
        finally: gh.close()

    def test_a_release_with_a_matching_list_is_verified_and_unpacked(self):
        r = self.run_get(release("0.3.16")); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn("checksum verified", r.stdout); self.assertIn("unpacked", r.stdout)

    def test_a_download_that_does_not_match_its_list_is_refused(self):
        r = self.run_get(release("0.3.16", sums="0" * 64 + "  truesense-0.3.16.tar.gz\n")); self.assertEqual(r.returncode, 1); self.assertIn("checksum mismatch", r.stderr)

    def test_a_list_that_does_not_name_the_file_is_refused(self):
        r = self.run_get(release("0.3.16", sums="a" * 64 + "  other.tar.gz\n")); self.assertEqual(r.returncode, 1); self.assertIn("does not list", r.stderr)

    def test_a_new_release_without_a_list_is_refused_not_skipped(self):
        r = self.run_get(release("0.3.16", sums=None, archive=True)); self.assertEqual(r.returncode, 1); self.assertIn("no SHA256SUMS", r.stderr)
        r = self.run_get(release("1.0.0", sums=None, archive=True), version="1.0.0"); self.assertEqual(r.returncode, 1)

    def test_an_old_release_without_a_list_still_installs_and_says_so(self):
        r = self.run_get(release("0.3.15", sums=None, asset=False, archive=True), version="0.3.15"); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn("predates published checksums", r.stdout)

    def test_a_server_error_on_the_list_is_an_error_not_a_fallback(self):
        r = self.run_get(release("0.3.15", sums=500, archive=True), version="0.3.15"); self.assertEqual(r.returncode, 1); self.assertIn("could not fetch the release checksums", r.stderr)

    def test_a_tarball_that_says_another_version_is_refused(self):
        r = self.run_get(release("0.3.16", inside="0.3.15")); self.assertEqual(r.returncode, 1); self.assertIn("refusing", r.stderr)

    def test_a_pinned_hash_must_also_match(self):
        r = self.run_get(release("0.3.16"), extra_env={"TRUESENSE_SHA256": "f" * 64}); self.assertNotEqual(r.returncode, 0)

    def test_the_newest_version_is_found_from_the_tags_when_none_is_pinned(self):
        files = {**release("0.3.16"), "/repos/Order52/truesense/tags": __import__("json").dumps([{"name": "v0.3.16"}, {"name": "v0.3.9"}, {"name": "v0.3.15"}, {"name": "junk"}], indent=2).encode()}      # GitHub pretty-prints the tag list: one name per line
        r = self.run_get(files, version=None); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn("TrueSense 0.3.16", r.stdout)

    def test_a_malformed_version_is_refused_before_anything_is_fetched(self):
        r = self.run_get({}, version="1.0; rm -rf /"); self.assertEqual(r.returncode, 1); self.assertIn("not a version", r.stderr)

    def test_it_insists_on_root_unless_told_otherwise(self):
        env = {k: v for k, v in os.environ.items() if k != "TRUESENSE_GET_ONLY"}
        r = subprocess.run(["bash", GET], capture_output=True, text=True, env=env, timeout=30)
        if os.getuid() != 0: self.assertEqual(r.returncode, 1); self.assertIn("run as root", r.stderr)


if __name__ == "__main__": unittest.main()
