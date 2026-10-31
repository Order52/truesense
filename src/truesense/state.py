# SPDX-License-Identifier: MIT
"""Persistent daemon settings (survive reboots): /var/lib/truesense/state.json, written atomically. Defaults come from the config."""
import json, os, threading

PATH = os.environ.get("TRUESENSE_STATE", "/var/lib/truesense/state.json")


class State:
    def __init__(self, defaults, path=PATH):
        self.path = path; self.lock = threading.Lock(); self.data = dict(defaults)
        try:
            with open(path) as f: self.data.update(json.load(f))
        except (OSError, ValueError): pass

    def get(self, key, default=None):
        with self.lock: return self.data.get(key, default)

    def set(self, key, value):
        with self.lock:
            self.data[key] = value
            try:
                os.makedirs(os.path.dirname(self.path), exist_ok=True)
                tmp = self.path + ".tmp"
                with open(tmp, "w") as f: json.dump(self.data, f)
                os.replace(tmp, self.path)
            except OSError: pass               # settings still apply for this boot if the disk is read-only

    def snapshot(self):
        with self.lock: return dict(self.data)
