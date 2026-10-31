"""Keyboard lighting (Chicony 04F2:0117, interface 3 / usage page 0xFF02) over hidraw feature reports (handoff section 19).

Packets are 8 bytes, checksum = 0xFF - (sum of bytes 0..6) & 0xFF, sent with HIDIOCSFEATURE (report id 0 prefix).
Effects: STATUS of write path = packets verified against a Windows capture; the visible result on Linux needs eyes (see docs/STATUS.md).
"""
import fcntl, glob, os, time
from . import util
from .backend import HwError

EFFECTS = {"off": 0, "static": 1, "breathing": 2, "wave": 3, "snake": 5, "ripple": 6, "neon": 8, "raindrop": 10, "lightning": 18,
           "spot": 37, "star": 38, "fireball": 39, "snow": 40, "heartbeat": 41}
NO_COLOR = {"off", "wave", "neon"}
MAX_BRIGHT = 50


def _chk(p): return (0xFF - sum(p)) & 0xFF

def _pkt(*b): return bytes(b) + bytes([_chk(bytes(b))])

def _hidraw():
    for h in sorted(glob.glob("/sys/class/hidraw/hidraw*")):
        try:
            if "000004F2:00000117" not in util.read(h + "/device/uevent").upper(): continue
            d = util.read(h + "/device/report_descriptor", "rb")
        except OSError: continue
        if d[:3] == b"\x06\x02\xff": return "/dev/" + os.path.basename(h)   # usage page 0xFF02
    raise HwError("keyboard lighting interface (usage page 0xFF02) not found")

SIM = {"brightness": 25, "packets": [], "frames": 0}      # TRUESENSE_SIM=1: a fake keyboard (the tests run the keyboard commands against it)

def _sim(): return bool(os.environ.get("TRUESENSE_SIM"))

def _send(pkts):
    if _sim():
        for p in pkts:
            SIM["packets"].append(bytes(p))
            if p[0] == 0x08: SIM["brightness"] = p[4]
        return
    node = _hidraw()
    try: fd = os.open(node, os.O_RDWR)
    except PermissionError: raise HwError(f"no permission on {node}: add yourself to group plugdev and install the udev rule")
    try:
        for p in pkts:
            buf = bytearray(b"\x00" + p)                              # report id 0 + 8 data bytes
            fcntl.ioctl(fd, 0xC0000000 | (len(buf) << 16) | (ord("H") << 8) | 0x06, buf)   # HIDIOCSFEATURE(len)
    finally: os.close(fd)

def set_effect(name, color="ffffff", speed=5, brightness=30, direction=0):
    if name == "operatingmode": return set_operating_mode(color)
    if name not in EFFECTS: raise HwError(f"effect: {', '.join(EFFECTS)}")
    if not 1 <= speed <= 11 or not 0 <= brightness <= MAX_BRIGHT: raise HwError(f"speed 1..11, brightness 0..{MAX_BRIGHT}")
    r, g, b = (0, 0, 0) if name in NO_COLOR else bytes.fromhex(color)
    pre = bytes([0xB1, 0, 0, 0, 0, 0, 0, 0x4E])
    if name == "off":
        _send([pre, _pkt(0x08, 0x02, 0, 0, 0, 0, 0), _pkt(0x14, 0, 0, 0, 0, 0, 0)]); return
    _send([pre, _pkt(0x14, 0, 0, r, g, b, 0), _pkt(0x08, 0x02, EFFECTS[name], (10 - speed) & 0xFF, brightness, 0x01, direction + 1)])

def _get_feature(fd, n=8):
    buf = bytearray(b"\x00" + bytes(n))
    fcntl.ioctl(fd, 0xC0000000 | (len(buf) << 16) | (ord("H") << 8) | 0x07, buf)   # HIDIOCGFEATURE(len)
    return bytes(buf[1:])

def get_brightness():
    """Current keyboard brightness (0..50) as the firmware reports it: byte 4 of the GET_REPORT after the `88` packet (verified: tracks writes)."""
    if _sim(): return SIM["brightness"]
    fd = os.open(_hidraw(), os.O_RDWR)
    try:
        buf = bytearray(b"\x00" + bytes([0x88, 0, 0, 0, 0, 0, 0, 0x77])); fcntl.ioctl(fd, 0xC0000000 | (len(buf) << 16) | (ord("H") << 8) | 0x06, buf)
        return _get_feature(fd)[4]
    finally: os.close(fd)

def per_key(colors, brightness=None):
    """Direct/per-key upload (handoff 19b, byte-identical to the Windows capture): 128 cells, 4 bytes each (00 R G B), 8 x 64-byte output reports.
    colors: list of up to 128 (r,g,b); missing cells are black. cell = col*6 + row (see re/reference/keymap_ph16-71_verified.json)."""
    if brightness is None:                                   # keep what the user set with Fn+F11/F12; never upload into darkness
        brightness = get_brightness() or 25
    if not 0 <= brightness <= MAX_BRIGHT: raise HwError(f"brightness 0..{MAX_BRIGHT}")
    cells = list(colors) + [(0, 0, 0)] * (128 - len(colors))
    data = b"".join(bytes([0, r, g, b]) for r, g, b in cells[:128])
    if _sim(): SIM["frames"] += 8; SIM["brightness"] = brightness; return
    node = _hidraw()
    try: fd = os.open(node, os.O_RDWR)
    except PermissionError: raise HwError(f"no permission on {node}: group plugdev + udev rule")
    try:
        def feat(p):
            buf = bytearray(b"\x00" + p); fcntl.ioctl(fd, 0xC0000000 | (len(buf) << 16) | (ord("H") << 8) | 0x06, buf)
        feat(bytes([0x88, 0, 0, 0, 0, 0, 0, 0x77])); _get_feature(fd)
        feat(bytes([0x12, 0, 0, 0x08, 0, 0, 0, 0xE5]))
        for i in range(8):
            os.write(fd, b"\x00" + data[i * 64:(i + 1) * 64]); time.sleep(0.016)   # leading 0 = report id: hidraw strips it, so the device gets exactly 64 data bytes     # Windows spaces frames ~15 ms apart; unpaced writes lose the last frame
        time.sleep(0.05)
        feat(_pkt(0x08, 0x02, 0x33, 0x05, brightness, 0x08, 0x01))
    finally: os.close(fd)

def restore_default(brightness=12):
    """The state PredatorSense left on Windows: cells 0..125 = #00aec7, brightness 12 (identical frames to re/battery_ui.pcapng)."""
    per_key([(0x00, 0xAE, 0xC7)] * 126, brightness)

def set_operating_mode(color="00aec7", speed=1):
    """OPERATINGMODE (0x22): the keyboard colour reflects the power mode (handoff 19a: no prelude; colour packet 14 00 01 R G B 00 chk'; effect 08 03 22 05 32 02 01 chk)."""
    r, g, b = hexc(color)
    _send([_pkt(0x14, 0, 0x01, r, g, b, 0), _pkt(0x08, 0x03, 0x22, 0x05, 0x32, 0x02, 0x01)])

# Acer's per-mode colours are not in the captured data/app: these defaults are OURS, overridable in config [keyboard.operating_colors].
OPERATING_COLORS = {"eco": "00cc66", "quiet": "3366ff", "balanced": "00aec7", "performance": "ff9900", "turbo": "ff0000"}

def follow_mode(mode, colors=None):
    """Operating-mode lighting that tracks the power mode: the keyboard does not do it by itself (verified), so the colour is re-sent on each change."""
    c = {**OPERATING_COLORS, **(colors or {})}
    set_operating_mode(c.get(mode, "00aec7"))

def hexc(c):
    import re
    c = c.lstrip("#")
    if not re.fullmatch(r"[0-9a-fA-F]{6}", c): raise HwError("colour must be RRGGBB")
    return tuple(bytes.fromhex(c))

def set_brightness(level):
    """Brightness of the current Direct/per-key state (final packet of the captured upload sequence)."""
    if not 0 <= level <= MAX_BRIGHT: raise HwError(f"brightness 0..{MAX_BRIGHT}")
    _send([_pkt(0x08, 0x02, 0x33, 0x05, level, 0x08, 0x01)])
