"""Compare recovered archive objects to recompiled source, ignoring producers only."""
import hashlib
import json
from pathlib import Path
import sys


def ar_members(path):
    data = path.read_bytes()
    if data[:8] != b"!<arch>\n":
        raise ValueError("not an ar archive")
    offset, names = 8, b""
    while offset < len(data):
        header = data[offset:offset + 60]
        name = header[:16].decode().strip()
        length = int(header[48:58])
        payload = data[offset + 60:offset + 60 + length]
        offset += 60 + length + length % 2
        if name == "//":
            names = payload
            continue
        if name == "/":
            continue
        if name.startswith("#1/"):
            count = int(name[3:])
            name, payload = payload[:count].rstrip(b"\0").decode(), payload[count:]
        elif name.startswith("/") and name[1:].isdigit():
            name = names[int(name[1:]):].split(b"/\n", 1)[0].decode()
        else:
            name = name.rstrip("/")
        yield name, payload


def sections(data):
    if data[:8] != b"\0asm\x01\0\0\0":
        raise ValueError("not a Wasm v1 object")

    def leb(offset):
        value, shift = 0, 0
        while True:
            byte = data[offset]
            offset += 1
            value |= (byte & 127) << shift
            if byte < 128:
                return value, offset
            shift += 7

    offset, result = 8, []
    while offset < len(data):
        section_id = data[offset]
        length, start = leb(offset + 1)
        end = start + length
        name = str(section_id)
        if section_id == 0:
            count, index = leb(start)
            name = data[index:index + count].decode()
        result.append((name, data[start:end]))
        offset = end
    return result


def normalized(data):
    # Preserve every byte of CODE, DATA, symbols, relocations and target features.
    # Only the distribution's compiler identity string is excluded.
    return tuple((name, payload) for name, payload in sections(data) if name != "producers")


def main():
    original, rebuilt, destination = map(Path, sys.argv[1:])
    index = {}
    for path in sorted(rebuilt.rglob("*.o")):
        data = path.read_bytes()
        index.setdefault(normalized(data), []).append(path.relative_to(rebuilt).as_posix())
    matched, unmatched = [], []
    for name, data in ar_members(original):
        candidates = index.get(normalized(data), [])
        row = dict(object=name, sha256=hashlib.sha256(data).hexdigest())
        if candidates:
            row["rebuilt_objects"] = candidates
            matched.append(row)
        else:
            unmatched.append(row)
    report = dict(ignored_section="producers", matched=len(matched), unmatched=len(unmatched),
                  rebuilt_objects=sum(len(paths) for paths in index.values()),
                  matched_objects=matched, unmatched_objects=unmatched)
    destination.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps({key: report[key] for key in ("matched", "unmatched", "rebuilt_objects")}))


if __name__ == "__main__":
    main()
