# SPDX-License-Identifier: MIT
"""Access to the TrueSense kernel helper (/dev/truesense).

One text command per call: open, write, read the reply, close. Every open file has its own reply buffer in the kernel, so the daemon, CLI and GUI
can use the device at the same time. Replies are "ok key=value ..." or "err message".

TRUESENSE_SIM=1 swaps the device for an in-memory firmware simulator (same protocol): used by the unit tests and for trying the tools without hardware.
"""
import os, threading

DEV = os.environ.get("TRUESENSE_DEVICE", "/dev/truesense")


class HwError(RuntimeError):
    pass


def parse(reply):
    """'ok a=1 b=0x2' -> {'a': '1', 'b': '0x2'}; raises HwError on 'err ...'."""
    reply = reply.strip()
    if reply.startswith("err"):
        raise HwError(reply[3:].strip() or "error")
    if not reply.startswith("ok"):
        raise HwError(f"unexpected reply {reply!r}")
    return dict(kv.split("=", 1) for kv in reply[2:].split() if "=" in kv)


def _real(command):
    try:
        fd = os.open(DEV, os.O_RDWR)
    except FileNotFoundError:
        raise HwError(f"{DEV} not found: the truesense_wmi kernel module is not loaded (sudo modprobe truesense_wmi, or run: truesense doctor)")
    except PermissionError:
        raise HwError(f"no permission on {DEV} (log in on the laptop's seat, or use sudo; see: truesense doctor)")
    try:
        try:
            os.write(fd, command.encode())
        except OSError as e:
            reply = os.read(fd, 256).decode(errors="replace")
            raise HwError(reply.strip()[3:].strip() or f"'{command}' rejected: {os.strerror(e.errno)}")
        return os.read(fd, 512).decode(errors="replace")
    finally:
        os.close(fd)


def cmd(command):
    """Run one whitelisted command, return the parsed reply as a dict of strings."""
    return parse(SIM.run(command) if os.environ.get("TRUESENSE_SIM") else _real(command))


def ival(d, key, base=10):
    try: return int(d[key], base if not d[key].startswith("0x") else 16)
    except (KeyError, ValueError): raise HwError(f"missing/invalid '{key}' in reply {d}")


# ---- firmware simulator (mirrors kernel/truesense-wmi; used by tests and TRUESENSE_SIM=1) ----------------------------------------------

class _Sim:
    def __init__(self): self.lock = threading.Lock(); self.reset()

    def reset(self):
        self.mode = 6; self.fan = "auto"; self.fan_duty = [30, 30]; self.gpu = 2; self.boot = 0; self.overdrive = 1; self.usb = 30; self.kbtimeout = 1
        self.battery = {1: 0, 2: 0}; self.rear = bytes.fromhex("00000300000100aec703000000000000"); self.zones = {}; self.temps = (50, 52, 48); self.rpm = (0, 0)

    def run(self, command):
        with self.lock:
            name, _, arg = command.strip().partition(" ")
            takes = {"set_mode", "set_gpu", "set_boot", "get_misc", "set_overdrive", "set_fan_manual", "set_rear", "set_color", "set_usb", "set_kbtimeout", "get_battery", "set_battery"}
            if name in takes and not arg.strip(): return "err missing argument\n"
            if name not in takes and arg.strip(): return "err unexpected argument\n"
            try: return getattr(self, "c_" + name)(arg.strip())
            except AttributeError: return "err unknown command\n"
            except (ValueError, IndexError): return "err invalid argument\n"

    def c_version(self, a): return "ok version=sim\n"
    def c_caps(self, a): return "ok game=1 apge=1 battery=1\n"
    def c_sensors(self, a): return f"ok cpu={self.temps[0]} sys={self.temps[1]} gpu={self.temps[2]} cpu_rpm={self.rpm[0]} gpu_rpm={self.rpm[1]}\n"
    def c_get_mode(self, a): return f"ok mode={self.mode}\n"
    def c_get_modes(self, a): return "ok mask=0x73\n"
    def c_set_mode(self, a):
        if int(a) not in (0, 1, 4, 5, 6): return "err invalid argument\n"
        self.mode = int(a); return f"ok mode={self.mode}\n"
    def c_get_gpu(self, a): return f"ok gpu={self.gpu}\n"
    def c_set_gpu(self, a):
        if int(a) not in (1, 2, 3): return "err invalid argument\n"
        self.gpu = int(a); return f"ok gpu={self.gpu}\n"
    def c_get_boot(self, a): return f"ok boot={self.boot}\n"
    def c_set_boot(self, a):
        if int(a) not in (0, 1): return "err invalid argument\n"
        self.boot = int(a); return f"ok boot={self.boot}\n"
    def c_get_overdrive(self, a): return f"ok overdrive={self.overdrive} raw=0x0\n"
    def c_set_overdrive(self, a):
        if int(a) not in (0, 1): return "err invalid argument\n"
        self.overdrive = int(a); return f"ok overdrive={self.overdrive}\n"
    def c_get_misc(self, a):
        if not 1 <= int(a) <= 16: return "err invalid argument\n"
        return f"ok idx={a} status=0 value=0\n"
    def c_get_fan(self, a):
        beh = {"auto": 0x41, "max": 0x82, "manual": 0xC3}[self.fan]
        return f"ok behavior=0x{beh:x} cpu={self.fan_duty[0]} gpu={self.fan_duty[1]}\n"
    def c_set_fan_auto(self, a): self.fan = "auto"; return "ok fan=auto\n"
    def c_set_fan_max(self, a): self.fan = "max"; return "ok fan=max\n"
    def c_set_fan_manual(self, a):
        c, g = (int(x) for x in a.split())
        if any(v != 0 and not 20 <= v <= 100 for v in (c, g)): return "err invalid argument\n"
        self.fan = "manual"; self.fan_duty = [c, g]; return f"ok fan=manual cpu={c} gpu={g}\n"
    def c_get_rear(self, a): return f"ok raw={self.rear.hex()}\n"
    def c_get_slot(self, a): return "ok raw=00000564000100aec700000000000000\n"
    def c_set_rear(self, a):
        b = bytes.fromhex(a)
        if len(b) != 16 or not (b[0] <= 3 or b[0] == 7) or not 1 <= b[1] <= 5 or b[2] > 100 or b[3] or b[4] > 1 or b[8] != 3 or b[9] != 2 or any(b[10:]): return "err invalid argument\n"
        self.rear = b"\x00" + b[:15]; return "ok\n"
    def c_set_color(self, a):
        p = a.split(" ")
        if len(p[0]) != 6 or len(p) > 2 or (len(p) == 2 and p[1] not in ("1", "2", "4")): return "err invalid argument\n"
        bytes.fromhex(p[0]); self.zones[int(p[1]) if len(p) == 2 else 1] = p[0]; return "ok\n"
    def c_get_usb(self, a): return f"ok usb={self.usb} raw=0\n"
    def c_set_usb(self, a):
        if int(a) not in (0, 10, 20, 30): return "err invalid argument\n"
        self.usb = int(a); return f"ok usb={self.usb}\n"
    def c_get_kbtimeout(self, a): return f"ok kbtimeout={self.kbtimeout} raw=0x0\n"
    def c_set_kbtimeout(self, a):
        if int(a) not in (0, 1): return "err invalid argument\n"
        self.kbtimeout = int(a); return f"ok kbtimeout={self.kbtimeout}\n"
    def c_get_battery(self, a):
        if int(a) not in (1, 2): return "err invalid argument\n"
        return f"ok battery={self.battery[int(a)]} list=3\n"
    def c_set_battery(self, a):
        w, v = (int(x) for x in a.split())
        if w not in (1, 2) or v not in (0, 1): return "err invalid argument\n"
        self.battery[w] = v; return f"ok battery={w} value={v}\n"

SIM = _Sim()
