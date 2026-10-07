"""Offline monitor documentation and kernel-replay gates; standard library only."""

import json
from pathlib import Path
import re
import sys


def require(condition, message):
    if not condition:
        raise ValueError(message)


def check_tracker(plan, status):
    expected = re.findall(r"^#### (P\d+-T\d+)$", plan, re.M)
    rows = re.findall(
        r"^\| \[([ x])\] \| \[(P\d+-T\d+)\].*?\| "
        r"(not started|in progress|blocked|done|deferred) \|", status, re.M
    )
    require(expected and len(set(expected)) == len(expected), "duplicate/missing plan IDs")
    require(sorted(task for _, task, _ in rows) == sorted(expected), "tracker task IDs differ from plan")
    for box, task, state in rows:
        require((box == "x") == (state == "done"), f"{task}: checkbox/state mismatch")
    phases = re.findall(
        r"^\| (P\d+) \|[^\n]+?\| (not started|in progress|blocked|done|deferred) "
        r"\| (\d+)/(\d+) \|", status, re.M
    )
    require(sorted(p for p, *_ in phases) == sorted({t.split("-")[0] for t in expected}),
            "phase IDs differ from tasks")
    for phase, state, done, total in phases:
        tasks = [r for r in rows if r[1].startswith(phase + "-")]
        require(int(total) == len(tasks) and int(done) == sum(r[0] == "x" for r in tasks),
                f"{phase}: phase totals disagree with checklist")
        require(state != "done" or done == total, f"{phase}: premature completion")
    done = sum(box == "x" for box, _, _ in rows)
    require(f"{done} of {len(rows)} implementation tasks" in status, "headline count is stale")
    return f"{len(rows)} tasks, {done} done; phase totals and checkboxes consistent"


def check_links(path):
    text = path.read_text()
    require(all(line.rstrip() == line for line in text.splitlines()), f"{path}: trailing whitespace")
    for link in re.findall(r"\]\(([^)]+)\)", text):
        if "://" in link:
            continue
        rel, _, anchor = link.partition("#")
        dest = path.parent / rel if rel else path
        require(dest.exists(), f"{path}: broken link {link}")
        if anchor:
            headings = [re.sub(r"[^\w\- ]", "", h.lower()).replace(" ", "-")
                        for h in re.findall(r"^#+ (.+)$", dest.read_text(), re.M)]
            require(anchor in headings, f"{path}: broken anchor {link}")


def check_docs(root):
    base = root / "cmd/go-link-monitor"
    print(check_tracker((base / "IMPLEMENTATION-PLAN.md").read_text(),
                        (base / "STATUS.md").read_text()))
    paths = sorted(base.glob("*.md"))
    for path in paths:
        check_links(path)
    metric = (base / "METRICS.md").read_text()
    definitions, examples = metric.split("## Alert and dashboard examples", 1)
    for name in set(re.findall(r"go_link_monitor_[a-z_]+", examples)):
        suffix = name.removeprefix("go_link_monitor_")
        require(name in definitions or f"`{suffix}`" in definitions, f"undefined alert metric {name}")
    print(f"PASS: {len(paths)} documents; links, anchors, whitespace and alert metric references")


def check_replay(events):
    roots = {"TestLinkStateKernelFixtures", "TestLinkStateRouteKernelFixtures"}
    passed = set()
    leaves = set()
    for row in events:
        require(row.get("Action") not in {"fail", "skip"}, "replay failed or skipped")
        if row.get("Action") == "pass":
            name = row.get("Test", "")
            if name in roots:
                passed.add(name)
            if name.split("/")[0] in roots and name.count("/") == 2:
                require(name not in leaves, f"duplicate replay outcome {name}")
                leaves.add(name)
    require(passed == roots and len(leaves) == 40, f"expected both replay suites and 40 leaves, got {len(leaves)}")
    print("PASS: all 40 kernel/scenario replay combinations; no skips")


if __name__ == "__main__":
    try:
        mode, target = sys.argv[1:]
        if mode == "docs":
            check_docs(Path(target))
        elif mode == "replay":
            check_replay(json.loads(line) for line in Path(target).read_text().splitlines())
        else:
            raise ValueError(f"unknown check {mode}")
    except (ValueError, OSError) as error:
        sys.exit(str(error))
