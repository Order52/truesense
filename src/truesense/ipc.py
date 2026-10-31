"""Local control socket of the daemon: one JSON object per line, one reply per request.

The daemon (root) owns the privileged and persistent settings; the CLI, GUI and key service (running as the user) talk to it here instead of sharing files.
The socket is world-connectable but every request is authorised by the caller's uid (SO_PEERCRED): root, or the configured desktop user.
"""
import json, os, socket, struct, threading
from .device import HwError

SOCK = os.environ.get("TRUESENSE_SOCKET", "/run/truesense/truesense.sock")
MAX = 65536


class Server(threading.Thread):
    def __init__(self, handlers, allowed_uids, path=SOCK):
        super().__init__(daemon=True, name="ipc"); self.handlers = handlers; self.allowed = set(allowed_uids) | {0}; self.path = path; self.sock = None

    def start(self):
        os.makedirs(os.path.dirname(self.path), exist_ok=True)
        try: os.unlink(self.path)
        except FileNotFoundError: pass
        old = os.umask(0)
        try:
            self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM); self.sock.bind(self.path); self.sock.listen(8)
        finally: os.umask(old)
        super().start()

    def stop(self):
        try:
            if self.sock: self.sock.close()
            os.unlink(self.path)
        except OSError: pass

    def run(self):
        while True:
            try: conn, _ = self.sock.accept()
            except OSError: return
            threading.Thread(target=self._serve, args=(conn,), daemon=True).start()

    def _serve(self, conn):
        with conn:
            conn.settimeout(3)
            try:
                uid = struct.unpack("3i", conn.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, struct.calcsize("3i")))[1]
                req = json.loads(conn.makefile("rb").readline(MAX) or b"{}")
                if uid not in self.allowed: out = {"ok": False, "error": f"uid {uid} is not allowed to control TrueSense"}
                else:
                    fn = self.handlers.get(req.get("cmd"))
                    out = {"ok": False, "error": f"unknown command {req.get('cmd')!r}"} if not fn else {"ok": True, "result": fn(**req.get("args", {}))}
            except HwError as e: out = {"ok": False, "error": str(e)}
            except Exception as e: out = {"ok": False, "error": f"{type(e).__name__}: {e}"}
            try: conn.sendall(json.dumps(out).encode() + b"\n")
            except OSError: pass


def available(path=SOCK): return os.path.exists(path)


def call(cmd, timeout=3, path=SOCK, **args):
    """Ask the daemon; returns the result or raises HwError."""
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as s:
            s.settimeout(timeout); s.connect(path); s.sendall(json.dumps({"cmd": cmd, "args": args}).encode() + b"\n")
            reply = json.loads(s.makefile("rb").readline(MAX) or b"{}")
    except FileNotFoundError: raise HwError("the TrueSense daemon is not running (sudo systemctl start truesense)")
    except PermissionError: raise HwError("no permission to reach the TrueSense daemon socket")
    except (OSError, ValueError) as e: raise HwError(f"daemon not answering: {e}")
    if not reply.get("ok"): raise HwError(reply.get("error", "daemon error"))
    return reply.get("result")
