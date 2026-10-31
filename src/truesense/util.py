"""Tiny file helpers that always close the file (a long-running daemon must not leak descriptors)."""


def read(path, mode="r"):
    with open(path, mode) as f: return f.read()


def write(path, data, mode="w"):
    with open(path, mode) as f: f.write(data)
