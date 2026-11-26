"""docs/netlink documentation gates; standard library only.

The rules are goip-parity-events.md Tier UB0-T02: every markdown link under
docs/netlink resolves (relative path and anchor), and the iproute2-findings
record under docs/netlink/upstream is complete — the index lists every
artifact, and every note is linked from a reader outside upstream/, so a
note cannot rot unread.
"""

import re
import sys
from pathlib import Path


def require(condition, message):
    if not condition:
        raise ValueError(message)


def anchor_of(heading):
    return re.sub(r"[^\w\- ]", "", heading.lower()).replace(" ", "-")


def check_links(path):
    """Trailing whitespace and relative links. Returns the markdown links."""
    text = path.read_text()
    for line in text.splitlines():
        require(line.rstrip() == line, f"{path}: trailing whitespace")
    links = re.findall(r"\]\(([^)]+)\)", text)
    for link in links:
        if "://" in link:
            continue
        rel, _, anchor = link.partition("#")
        dest = path.parent / rel if rel else path
        require(dest.exists(), f"{path}: broken link {link}")
        if anchor:
            pattern = re.findall(r"^#+ (.*)$", dest.read_text(), re.M)
            headings = [anchor_of(h) for h in pattern]
            require(anchor in headings, f"{path}: broken anchor {link}")
    return links


def check_upstream(base):
    """The index lists every artifact; every note has a reader outside it."""
    upstream = base / "upstream"
    index = upstream / "README.md"
    require(index.exists(), f"{index}: the upstream index is missing")
    listed = index.read_text()
    for entry in sorted(upstream.iterdir()):
        if entry == index:
            continue
        require(entry.name in listed, f"{index}: does not list {entry.name}")
    readers = [
        path.read_text()
        for path in base.rglob("*.md")
        if upstream not in path.parents
    ]
    notes = 0
    for note in sorted(upstream.glob("*.md")):
        if note == index:
            continue
        notes += 1
        # A reader links the note by name, at any relative depth.
        linked = re.compile(r"\]\([^)]*" + re.escape(note.name) + r"\)")
        require(any(linked.search(text) for text in readers),
                f"{note}: linked from nowhere outside the index")
    return len(list(upstream.iterdir())), notes


def check_monitor_blobs(base, root):
    """Every committed monitor blob file is named from a document.

    The crafted .nl blobs under internal/goip/testdata/monitor are EP2's
    artifact class (goip-parity-events.md, EP2-T03's gate): each is the
    four files a blob is — the spec, the .nl, and the two measured goldens
    — so the rule is the upstream index's own: a file no document names
    is a file the record has lost, and it fails the check rather than rot.
    """
    monitor = root / "internal" / "goip" / "testdata" / "monitor"
    require(monitor.is_dir(), f"{monitor}: the blob set is missing")
    docs = [path.read_text() for path in base.rglob("*.md")]
    files = sorted(entry.name for entry in monitor.iterdir())
    for name in files:
        require(any(name in text for text in docs),
                f"{name}: named from no document under docs/netlink")
    return len(files)


def check_docs(root):
    base = root / "docs" / "netlink"
    paths = sorted(base.rglob("*.md"))
    for path in paths:
        check_links(path)
    artifacts, notes = check_upstream(base)
    blobs = check_monitor_blobs(base, root)
    print(f"PASS: {len(paths)} documents; links, anchors and whitespace;"
          f" the upstream index lists all {artifacts} artifacts,"
          f" all {notes} notes linked; {blobs} committed blob files named")


if __name__ == "__main__":
    try:
        mode, target = sys.argv[1:]
        if mode != "docs":
            raise ValueError(f"unknown check {mode}")
        check_docs(Path(target))
    except (ValueError, OSError) as error:
        sys.exit(str(error))
