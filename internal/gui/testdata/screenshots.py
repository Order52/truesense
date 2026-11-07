#!/usr/bin/env python3
"""Screenshots and a click-through of the window, in headless Firefox with a throw-away profile.

    go build -o /tmp/guidev ./internal/gui/testdata/devserver
    python3 internal/gui/testdata/screenshots.py OUTDIR [pages] [schemes] [sizes]

pages   comma list, default every page (home,lighting,lighting/keys,...)
schemes light,dark (the browser's prefers-color-scheme), default both
sizes   w = 1280 wide, n = narrow (Firefox will not go below 488 px)

Needs only firefox and python3. It talks to Firefox over Marionette (the protocol of
geckodriver) directly, so nothing has to be installed. It never touches your profile: the
profile is a temporary directory, and the server is the simulated laptop of
internal/gui/guitest. The window URL holds a one-time code, so every browser start
gets its own server.
"""
import base64, json, os, shutil, socket, subprocess, sys, tempfile, time

DEVSERVER = os.environ.get("GUIDEV", "/tmp/guidev")
PAGES = ["home", "lighting", "lighting/keys", "lighting/rear", "lighting/profiles", "curves", "smart", "predator", "undervolt", "battery", "audio", "profiles", "health", "settings"]
SIZES = {"w": (1280, 800), "n": (488, 1000)}


class Firefox:
    def __init__(self, dark, port=2828):
        self.profile = tempfile.mkdtemp(prefix="ffprof-")
        with open(os.path.join(self.profile, "user.js"), "w") as f:
            f.write(f'user_pref("layout.css.prefers-color-scheme.content-override", {0 if dark else 1});\n')
            f.write(f'user_pref("marionette.port", {port});\nuser_pref("browser.shell.checkDefaultBrowser", false);\n')
        self.proc = subprocess.Popen(["firefox", "--headless", "--no-remote", "--marionette", "--profile", self.profile], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.id, self.buf = 0, b""
        for _ in range(100):
            try:
                self.sock = socket.create_connection(("127.0.0.1", port), timeout=30)
                break
            except OSError:
                time.sleep(0.2)
        else:
            raise RuntimeError("Firefox did not start")
        self._read()  # the hello
        self.cmd("WebDriver:NewSession", {"capabilities": {}})

    def _read(self):
        while b":" not in self.buf:
            self.buf += self.sock.recv(65536)
        n, rest = self.buf.split(b":", 1)
        n = int(n)
        while len(rest) < n:
            rest += self.sock.recv(65536)
        self.buf = rest[n:]
        return json.loads(rest[:n])

    def cmd(self, name, params=None):
        self.id += 1
        msg = json.dumps([0, self.id, name, params or {}]).encode()
        self.sock.sendall(str(len(msg)).encode() + b":" + msg)
        while True:
            r = self._read()
            if r[0] == 1 and r[1] == self.id:
                if r[2]:
                    raise RuntimeError(f"{name}: {r[2]}")
                return r[3]

    def js(self, script, args=None):
        return self.cmd("WebDriver:ExecuteScript", {"script": script, "args": args or []}).get("value")

    def shot(self, path, full=True):
        data = self.cmd("WebDriver:TakeScreenshot", {"full": full, "hash": False, "scroll": False})["value"]
        with open(path, "wb") as f:
            f.write(base64.b64decode(data))

    def close(self):
        try:
            self.cmd("Marionette:Quit")
        except Exception:
            pass
        try:
            self.proc.wait(10)
        except Exception:
            self.proc.kill()
        shutil.rmtree(self.profile, ignore_errors=True)


def main():
    out = sys.argv[1]
    pages = sys.argv[2].split(",") if len(sys.argv) > 2 and sys.argv[2] else PAGES
    schemes = sys.argv[3].split(",") if len(sys.argv) > 3 else ["light", "dark"]
    sizes = sys.argv[4].split(",") if len(sys.argv) > 4 else ["w", "n"]
    os.makedirs(out, exist_ok=True)
    for scheme in schemes:
        for size in sizes:
            dev = subprocess.Popen([DEVSERVER, "-idle", "20m"], stdout=subprocess.PIPE, text=True)
            url = dev.stdout.readline().strip()
            ff = Firefox(dark=scheme == "dark")
            try:
                ff.cmd("WebDriver:Navigate", {"url": url})
                ff.cmd("WebDriver:SetWindowRect", dict(zip(("width", "height"), SIZES[size])))
                for page in pages:
                    ff.js("location.hash = arguments[0]", [f"#/{page}"])
                    time.sleep(3.2)  # the page fetches its data and the first live readings arrive
                    ff.shot(os.path.join(out, f"{page.replace('/', '-')}-{scheme}-{size}.png"))
            finally:
                ff.close()
                dev.terminate()


if __name__ == "__main__":
    main()
