"""xtcp2-nlcap-batch: run a rows file of netlink capture rows in the guest.

The guest half of the nl_mon capture workbench
(docs/netlink/coverage-status.md, "Workbench and generated rows"). The
host CLI, xtcp2-nlcap-ws, copies a rows file in and runs this by store path
over one ssh call; it pulls OUTDIR back.

A rows file is JSON Lines, one row per line:

    {"id": "tc/fq-0001", "desc": "positive: ...", "cmd": "tc qdisc ...",
     "pre": ["ip link add d0 type dummy", ["ip link del x", 1]],
     "readback": [{"name": "qdisc", "cmd": "tc -j qdisc show dev d0"}],
     "proto": "route", "floor": 0, "modules": ["sch_fq"], "serial": false,
     "expect": {"rc": 0}}

Each row runs in a fresh network namespace with its own nlmon0, through the
same `xtcp2-nlcap` primitives the corpus drivers use: ns-add, write (the
command, inside the capture window) and capio (each readback). nlmon is per
network namespace, so rows in different namespaces cannot see each other's
messages, and `-j N` runs N of them at once. A row marked serial runs with no
other row active: a sysctl outside the namespace, or a module load, is
kernel-wide.

Every module any row names is loaded once, before the first row, so every
row of a batch sees one kernel state. A module loaded half way through would
give the rows either side of it different fallback devices and ifindexes.

--ip-bin DIR runs every row with DIR's `ip` instead of the guest's: DIR is
put first on PATH for the pre steps and readbacks, and exported as
NLCAP_IP_BIN, which xtcp2-nlcap puts first on its own PATH (its
writeShellApplication PATH would otherwise win). The guest sees the host's
/nix/store, so a store path's bin works with no reboot. That is how one
guest captures the same rows at two iproute2 versions
(docs/netlink/goip-parity-next.md, NC0-T01).

--bpf-obj FILE links FILE, a BPF ELF object, at --bpf-link (default
/run/nlcap-ws/lwt_bpf.o) before the first row, so a row's command names one
fixed path however the store path moves: `encap bpf in obj
/run/nlcap-ws/lwt_bpf.o sec lwt_in`. ip names the program by the path's
basename and section, so the readbacks do not move either (NC8-T02).

The guest's lwtunnel kernel options, read from --kconfig (default
/proc/config.gz), go into each row's env as "kconfig", so a corpus shows
whether its kernel had RPL and IOAM6 (NC8-T01).

Each row's directory gets cmd.json: the row as given, the kernel and
iproute2 versions, and the result. That is the record a table test reads.
Nothing in it depends on the clock, so a replay on a fresh boot diffs clean.

Exit status: 0 when every row ran (whatever its rc), 1 when a row could not
be captured or its rc differed from expect.rc, 2 on a bad rows file or
usage.
"""

import argparse
import concurrent.futures
import gzip
import json
import os
import re
import subprocess
import sys

ID_RE = re.compile(r"^[a-z0-9][a-z0-9_.+-]*(/[a-z0-9][a-z0-9_.+-]*)*$")
NAME_RE = re.compile(r"^[a-z0-9][a-z0-9_]*$")
PREFIXES = ("positive:", "negative:", "boundary:", "corner:")
PROTOS = ("route", "generic", "xfrm", "sock_diag")
KNOWN = {"id", "desc", "cmd", "pre", "readback", "proto", "floor",
         "modules", "serial", "expect", "axes", "source"}


class RowError(Exception):
    """A rows file that cannot be run as written."""


def fail(msg):
    print("NLCAP_ERR " + msg, file=sys.stderr)
    sys.exit(2)


def check_pre(where, pre):
    if not isinstance(pre, list):
        raise RowError(where + ": pre must be a list")
    out = []
    for step in pre:
        if isinstance(step, str):
            out.append((step, [0]))
        elif (isinstance(step, list) and len(step) == 2
              and isinstance(step[0], str)):
            rcs = step[1] if isinstance(step[1], list) else [step[1]]
            if not all(isinstance(r, int) for r in rcs):
                raise RowError(where + ": pre rc must be an int")
            out.append((step[0], [0] + rcs))
        else:
            raise RowError(where + ": pre step must be a string or "
                           "[cmd, rc]")
    return out


def check_readback(where, rbs):
    if not isinstance(rbs, list):
        raise RowError(where + ": readback must be a list")
    seen = set()
    for rb in rbs:
        if not isinstance(rb, dict) or not isinstance(rb.get("cmd"), str):
            raise RowError(where + ": readback needs name and cmd")
        name = rb.get("name", "")
        if not isinstance(name, str) or not NAME_RE.match(name):
            raise RowError(where + f": bad readback name {name!r}")
        if name == "ip" or name in seen:
            raise RowError(where + f": readback name {name!r} reused")
        if rb.get("proto", "route") not in PROTOS:
            raise RowError(where + f": readback proto {rb.get('proto')!r}")
        seen.add(name)


def check_row(lineno, row):
    where = f"line {lineno}"
    if not isinstance(row, dict):
        raise RowError(where + ": not an object")
    unknown = sorted(set(row) - KNOWN)
    if unknown:
        raise RowError(where + ": unknown field " + ", ".join(unknown))
    rid = row.get("id")
    if not isinstance(rid, str) or not ID_RE.match(rid):
        raise RowError(where + f": bad id {rid!r}")
    where = f"{where} ({rid})"
    desc = row.get("desc")
    if not isinstance(desc, str) or not desc.startswith(PREFIXES):
        raise RowError(where + ": desc must start with one of "
                       + " ".join(PREFIXES))
    cmd = row.get("cmd")
    if not isinstance(cmd, str) or not cmd.strip():
        raise RowError(where + ": no cmd")
    if row.get("proto", "route") not in PROTOS:
        raise RowError(where + f": proto {row.get('proto')!r}")
    floor = row.get("floor", 0)
    if not isinstance(floor, int) or isinstance(floor, bool) or floor < 0:
        raise RowError(where + ": floor must be an int >= 0")
    mods = row.get("modules", [])
    if not isinstance(mods, list) or not all(
            isinstance(m, str) and NAME_RE.match(m) for m in mods):
        raise RowError(where + ": modules must be a list of names")
    if not isinstance(row.get("serial", False), bool):
        raise RowError(where + ": serial must be true or false")
    check_readback(where, row.get("readback", []))
    return check_pre(where, row.get("pre", []))


def load_rows(path, only):
    """Parse and check a rows file; return [(row, pre)] in file order."""
    rows = []
    seen = {}
    with open(path, encoding="utf-8") as f:
        for lineno, line in enumerate(f, 1):
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            try:
                row = json.loads(line)
            except ValueError as e:
                raise RowError(f"line {lineno}: {e}")
            pre = check_row(lineno, row)
            if row["id"] in seen:
                raise RowError(f"line {lineno}: id {row['id']} repeats line "
                               f"{seen[row['id']]}")
            seen[row["id"]] = lineno
            rows.append((row, pre))
    if not rows:
        raise RowError("no rows in " + path)
    if only:
        missing = sorted(set(only) - set(seen))
        if missing:
            raise RowError("--only names unknown ids: " + ", ".join(missing))
        rows = [r for r in rows if r[0]["id"] in only]
    return rows


def segments(rows):
    """Split rows into runs of parallel rows and lone serial rows."""
    out, cur = [], []
    for r in rows:
        if r[0].get("serial", False):
            if cur:
                out.append(cur)
                cur = []
            out.append([r])
        else:
            cur.append(r)
    if cur:
        out.append(cur)
    return out


class Runner:
    def __init__(self, nlcap, ip, outdir, env, ip_bin=""):
        self.nlcap = nlcap
        self.ip = ip
        self.outdir = outdir
        self.env = env
        self.ip_bin = ip_bin

    def call(self, args, cwd=None):
        e = dict(os.environ, NLCAP_OUT=self.outdir)
        if self.ip_bin:
            e["NLCAP_IP_BIN"] = self.ip_bin
            e["PATH"] = self.ip_bin + os.pathsep + e.get("PATH", "")
        p = subprocess.run(args, cwd=cwd, env=e, capture_output=True,
                           text=True, check=False)
        return p.returncode, p.stdout + p.stderr

    def run_row(self, slot, row, pre):
        ns = f"ws{slot}"
        rid = row["id"]
        rdir = os.path.join(self.outdir, rid)
        os.makedirs(rdir, exist_ok=True)
        log = []
        result = {"status": "OK"}
        rc, out = self.call([self.nlcap, "ns-add", ns])
        log.append(out)
        if rc != 0:
            result["status"] = "NS_FAIL"
            return self.finish(row, rdir, result, log)
        pre_log = []
        for cmd, ok in pre:
            rc, out = self.call([self.ip, "netns", "exec", ns, "sh", "-c",
                                 cmd], cwd=rdir)
            pre_log.append({"cmd": cmd, "rc": rc})
            if rc not in ok:
                result["status"] = "PRE_FAIL"
                log.append(out)
                break
        if pre_log:
            result["pre"] = pre_log
        if result["status"] == "OK":
            self.capture(ns, row, rdir, result, log)
        self.call([self.nlcap, "ns-del", ns])
        return self.finish(row, rdir, result, log)

    def capture(self, ns, row, rdir, result, log):
        rid = row["id"]
        proto = row.get("proto", "route")
        rc, out = self.call([self.nlcap, "write", "--proto", proto, ns, rid,
                             "ip", str(row.get("floor", 0)), row["cmd"]])
        log.append(out)
        if rc != 0:
            result["status"] = "SHORT"
            return
        with open(os.path.join(rdir, "ip.rc"), encoding="utf-8") as f:
            result["rc"] = int(f.read().strip() or "-1")
        want = row.get("expect", {}).get("rc")
        if want is not None and want != result["rc"]:
            result["status"] = "RC_MISMATCH"
        for rb in row.get("readback", []):
            rbp = rb.get("proto", "route")
            name = f"netlink_{rbp}_{rb['name']}"
            rc, out = self.call([self.nlcap, "capio", "--proto", rbp, ns,
                                 rid, name, "0", rb["cmd"],
                                 rb["name"] + ".out"])
            log.append(out)
            if rc != 0:
                result.setdefault("readback_short", []).append(rb["name"])
                if result["status"] == "OK":
                    result["status"] = "SHORT"

    def finish(self, row, rdir, result, log):
        rec = dict(row)
        rec["env"] = self.env
        rec["result"] = result
        with open(os.path.join(rdir, "cmd.json"), "w",
                  encoding="utf-8") as f:
            json.dump(rec, f, indent=1, sort_keys=True)
            f.write("\n")
        if result["status"] != "OK":
            with open(os.path.join(rdir, "nlcap.log"), "w",
                      encoding="utf-8") as f:
                f.write("".join(log))
        return row["id"], result


def kconfig_lwt(path):
    """The kernel config's lwtunnel lines, sorted; [] when unreadable."""
    try:
        with gzip.open(path, "rt", encoding="utf-8") as f:
            return sorted({ln.strip() for ln in f if "LWTUNNEL" in ln})
    except (OSError, EOFError, ValueError):
        return []


def link_bpf(obj, link):
    """Point link at obj, replacing what was there; False if obj is no file."""
    if not os.path.isfile(obj):
        return False
    os.makedirs(os.path.dirname(link) or ".", exist_ok=True)
    tmp = link + ".tmp"
    if os.path.lexists(tmp):
        os.unlink(tmp)
    os.symlink(os.path.abspath(obj), tmp)
    os.replace(tmp, link)
    return True


def guest_env(ip, kconfig="", bpf_link=""):
    def out(args):
        try:
            p = subprocess.run(args, capture_output=True, text=True,
                               check=False)
        except OSError:
            return ""
        return p.stdout.strip()
    env = {"kernel": out(["uname", "-r"]),
           "iproute2": out([ip, "-V"])}
    lines = kconfig_lwt(kconfig) if kconfig else []
    if lines:
        env["kconfig"] = lines
    if bpf_link:
        env["lwt_bpf"] = bpf_link
    return env


def load_modules(rows, log):
    mods = sorted({m for r, _ in rows for m in r.get("modules", [])})
    bad = []
    for m in mods:
        p = subprocess.run(["modprobe", m], capture_output=True, text=True,
                           check=False)
        if p.returncode != 0:
            bad.append(m)
            print(f"NLCAP_MODULE_FAIL {m} {p.stderr.strip()}",
                  file=log)
    return mods, bad


def run_all(runner, rows, jobs):
    results = {}
    for seg in segments(rows):
        n = 1 if len(seg) == 1 else jobs
        with concurrent.futures.ThreadPoolExecutor(max_workers=n) as ex:
            slots = list(range(n))
            pending = {}
            queue = list(seg)
            while queue or pending:
                while queue and slots:
                    slot = slots.pop()
                    row, pre = queue.pop(0)
                    pending[ex.submit(runner.run_row, slot, row, pre)] = slot
                done, _ = concurrent.futures.wait(
                    pending, return_when=concurrent.futures.FIRST_COMPLETED)
                for fut in done:
                    slots.append(pending.pop(fut))
                    rid, res = fut.result()
                    results[rid] = res
                    rc = res.get('rc', '-')
                    print(f"NLCAP_ROW {res['status']} {rid} rc={rc}",
                          flush=True)
    return results


def main(argv=None):
    ap = argparse.ArgumentParser(prog="xtcp2-nlcap-batch")
    ap.add_argument("-j", "--jobs", type=int, default=8)
    ap.add_argument("--only", default="")
    ap.add_argument("--nlcap", default="xtcp2-nlcap")
    ap.add_argument("--ip", default="ip")
    ap.add_argument("--ip-bin", default="",
                    help="a directory whose ip every row runs")
    ap.add_argument("--bpf-obj", default="",
                    help="a BPF object to link at --bpf-link")
    ap.add_argument("--bpf-link", default="/run/nlcap-ws/lwt_bpf.o")
    ap.add_argument("--kconfig", default="/proc/config.gz")
    ap.add_argument("--no-modules", action="store_true",
                    help="skip modprobe (hermetic tests)")
    ap.add_argument("rows")
    ap.add_argument("outdir")
    a = ap.parse_args(argv)
    if a.jobs < 1 or a.jobs > 64:
        fail("-j must be 1..64")
    if a.ip_bin:
        a.ip_bin = os.path.abspath(a.ip_bin)
        a.ip = os.path.join(a.ip_bin, "ip")
        if not os.access(a.ip, os.X_OK):
            fail("no ip in " + a.ip_bin)
    if a.bpf_obj and not link_bpf(a.bpf_obj, a.bpf_link):
        fail("no bpf object " + a.bpf_obj)
    only = [s for s in a.only.split(",") if s]
    try:
        rows = load_rows(a.rows, only)
    except (RowError, OSError) as e:
        fail(str(e))
    os.makedirs(a.outdir, exist_ok=True)
    if not a.no_modules:
        _mods, bad = load_modules(rows, sys.stderr)
        if bad:
            fail("modules failed to load: " + ", ".join(bad))
    runner = Runner(a.nlcap, a.ip, os.path.abspath(a.outdir),
                    guest_env(a.ip, a.kconfig,
                              a.bpf_link if a.bpf_obj else ""), a.ip_bin)
    results = run_all(runner, rows, a.jobs)
    with open(os.path.join(a.outdir, ".batch-results.jsonl"), "w",
              encoding="utf-8") as f:
        for row, _ in rows:
            f.write(json.dumps({"id": row["id"],
                                **results[row["id"]]}, sort_keys=True))
            f.write("\n")
    bad = [r for r in results.values() if r["status"] != "OK"]
    print(f"NLCAP_BATCH_DONE rows={len(results)} "
          f"ok={len(results) - len(bad)} not_ok={len(bad)}", flush=True)
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
