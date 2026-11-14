"""Hermetic table tests for nlcap_batch.py (run by checks.nlcap-ws).

No VM and no root: xtcp2-nlcap and ip are stubs that log their argv and
fake the files a real write leaves behind.
"""

import gzip
import json
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from typing import ClassVar

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import nlcap_batch  # noqa: E402

# The stub helper: like the real one, takes --proto after the subcommand
# and refuses it before. Logs its argv and, for write/capio, leaves the
# files the real one would. A command containing RC=<n> exits the "tool"
# with n; one containing SLEEP sleeps first, so overlap is observable.
STUB_NLCAP = r"""#!/bin/sh
log="$NLCAP_OUT/../calls.log"
echo "$*" >> "$log"
echo "NLCAP_IP_BIN=${NLCAP_IP_BIN:-}" >> "$NLCAP_OUT/../nlcap-env.log"
[ "$1" = --proto ] && { echo "NLCAP_ERR unknown subcommand: --proto" >&2; exit 2; }
sub=$1; shift
[ "$1" = --proto ] && shift 2
case "$sub" in
  write)
    d="$NLCAP_OUT/$2"; mkdir -p "$d"
    case "$5" in *SLEEP*) echo "start $2" >> "$log"; sleep 0.3;
                          echo "end $2" >> "$log";; esac
    rc=0
    case "$5" in *RC=*) rc=$(echo "$5" | sed 's/.*RC=\([0-9]*\).*/\1/');; esac
    case "$5" in *SHORT*) exit 1;; esac
    : > "$d/ip.pcap"; echo "$rc" > "$d/ip.rc"; : > "$d/ip.out"
    ;;
  capio) d="$NLCAP_OUT/$2"; : > "$d/$3.pcap"; : > "$d/$6" ;;
esac
exit 0
"""

# The stub ip: `ip netns exec <ns> sh -c <cmd>` runs <cmd> here; -V prints
# a version.
STUB_IP = r"""#!/bin/sh
if [ "$1" = -V ]; then echo "ip utility, iproute2-stub"; exit 0; fi
echo "ip $*" >> "$NLCAP_OUT/../calls.log"
shift 3
exec "$@"
"""

# The --ip-bin ip: another version, and its own name in the calls log.
STUB_IP730 = r"""#!/bin/sh
if [ "$1" = -V ]; then echo "ip utility, iproute2-7.3.0-stub"; exit 0; fi
echo "ip730 $*" >> "$NLCAP_OUT/../calls.log"
shift 3
exec "$@"
"""

FULL = {"id": "t/full", "desc": "positive: full row",
        "cmd": "ip link add d0 type dummy",
        "pre": ["true", ["exit 2", 2]],
        "readback": [{"name": "link", "cmd": "ip -j link"}],
        "expect": {"rc": 0}}


def row(**kw):
    r = {"id": "t/r", "desc": "positive: r", "cmd": "ip link"}
    r.update(kw)
    return r


class Env:
    def __init__(self, tc, rows, raw=None):
        self.tmp = tempfile.TemporaryDirectory()
        tc.addCleanup(self.tmp.cleanup)
        d = self.tmp.name
        self.out = os.path.join(d, "out")
        self.rows = os.path.join(d, "rows.jsonl")
        self.log = os.path.join(d, "calls.log")
        with open(self.rows, "w") as f:
            if raw is not None:
                f.write(raw)
            f.writelines(json.dumps(r) + "\n" for r in rows)
        self.nlcap = self.stub("nlcap", STUB_NLCAP)
        self.ip = self.stub("ip", STUB_IP)

    def stub(self, name, text):
        p = os.path.join(self.tmp.name, name)
        with open(p, "w") as f:
            f.write(text)
        os.chmod(p, stat.S_IRWXU)
        return p

    def run(self, *extra):
        os.makedirs(self.out, exist_ok=True)
        args = ["--no-modules", "--nlcap", self.nlcap, "--ip", self.ip,
                *extra, self.rows, self.out]
        p = subprocess.run([sys.executable, "-I", nlcap_batch.__file__,
                            *args], capture_output=True, text=True,
                           env=dict(os.environ, NLCAP_OUT=self.out),
                           timeout=60, check=False)
        return p.returncode, p.stdout + p.stderr

    def calls(self):
        if not os.path.exists(self.log):
            return []
        with open(self.log) as f:
            return [ln.split()[0] for ln in f.read().splitlines()]

    def cmd_json(self, rid):
        with open(os.path.join(self.out, rid, "cmd.json")) as f:
            return json.load(f)


class TestRowsFile(unittest.TestCase):
    """Rows-file checks: each must exit 2 with NLCAP_ERR naming the fault."""

    CASES: ClassVar[list[tuple]] = [
        ("negative: no cmd", [row(cmd="")], None, "no cmd"),
        ("negative: an id escaping the out dir", [row(id="../x")], None,
         "bad id"),
        ("negative: an absolute id", [row(id="/x")], None, "bad id"),
        ("negative: a desc without a class", [row(desc="full row")], None,
         "desc must start"),
        ("negative: an unknown field", [row(bogus=1)], None,
         "unknown field bogus"),
        ("negative: a bad proto", [row(proto="rdma")], None, "proto"),
        ("negative: a negative floor", [row(floor=-1)], None, "floor"),
        ("negative: a readback named ip", [row(readback=[
            {"name": "ip", "cmd": "x"}])], None, "reused"),
        ("negative: not JSON", [], "{nope\n", "line 1"),
        ("corner: a repeated id", [row(), row()], None,
         "id t/r repeats line 1"),
        ("boundary: an empty rows file", [], "", "no rows"),
        ("boundary: comments only", [], "# x\n\n", "no rows"),
    ]

    def test_cases(self):
        for desc, rows, raw, want in self.CASES:
            with self.subTest(desc):
                e = Env(self, rows, raw)
                rc, out = e.run()
                self.assertEqual(rc, 2, out)
                self.assertIn("NLCAP_ERR", out)
                self.assertIn(want, out)
                self.assertEqual(e.calls(), [], "no row may run")

    def test_only_unknown(self):
        e = Env(self, [row()])
        rc, out = e.run("--only", "t/nosuch")
        self.assertEqual(rc, 2)
        self.assertIn("unknown ids: t/nosuch", out)


class TestRun(unittest.TestCase):
    """Rows that run: the calls made, the status and cmd.json."""

    CASES: ClassVar[list[tuple]] = [
        # (description, rows, want exit, want call kinds, want status)
        ("positive: a full row", [FULL], 0,
         ["ns-add", "ip", "ip", "write", "capio", "ns-del"], "OK"),
        ("boundary: no pre, no readback", [row()], 0,
         ["ns-add", "write", "ns-del"], "OK"),
        ("negative: a pre step fails", [row(pre=["exit 3"])], 1,
         ["ns-add", "ip", "ns-del"], "PRE_FAIL"),
        ("negative: rc differs from expect",
         [row(cmd="x RC=2", expect={"rc": 0})], 1,
         ["ns-add", "write", "ns-del"], "RC_MISMATCH"),
        ("positive: a refusal that was expected",
         [row(cmd="x RC=255", expect={"rc": 255})], 0,
         ["ns-add", "write", "ns-del"], "OK"),
        ("negative: the capture came in short", [row(cmd="x SHORT")], 1,
         ["ns-add", "write", "ns-del"], "SHORT"),
    ]

    def test_cases(self):
        for desc, rows, want_rc, want_calls, want_status in self.CASES:
            with self.subTest(desc):
                e = Env(self, rows)
                rc, out = e.run("-j", "1")
                self.assertEqual(rc, want_rc, out)
                self.assertEqual(e.calls(), want_calls)
                got = e.cmd_json(rows[0]["id"])
                self.assertEqual(got["result"]["status"], want_status)
                self.assertEqual(got["cmd"], rows[0]["cmd"])
                self.assertEqual(got["env"]["iproute2"],
                                 "ip utility, iproute2-stub")

    def test_full_row_record(self):
        e = Env(self, [FULL])
        e.run()
        got = e.cmd_json("t/full")
        self.assertEqual(got["result"]["pre"],
                         [{"cmd": "true", "rc": 0},
                          {"cmd": "exit 2", "rc": 2}])
        self.assertEqual(got["result"]["rc"], 0)
        self.assertTrue(os.path.exists(os.path.join(
            e.out, "t/full", "netlink_route_link.pcap")))
        self.assertTrue(os.path.exists(os.path.join(
            e.out, "t/full", "link.out")))

    def test_proto_reaches_the_helper(self):
        cases = [
            ("positive: a sock_diag row's write and readback name it",
             row(proto="sock_diag", readback=[
                 {"name": "dump", "cmd": "ss -t", "proto": "sock_diag"}]),
             ["write --proto sock_diag", "capio --proto sock_diag"]),
            ("boundary: no proto is route", row(readback=[
                {"name": "link", "cmd": "ip -j link"}]),
             ["write --proto route", "capio --proto route"]),
        ]
        for desc, r, want in cases:
            with self.subTest(desc):
                e = Env(self, [r])
                rc, out = e.run()
                self.assertEqual(rc, 0, out)
                with open(e.log) as f:
                    got = [" ".join(ln.split()[:3]) for ln in f.read().splitlines()
                           if ln.split()[0] in ("write", "capio")]
                self.assertEqual(got, want)

    def test_only(self):
        e = Env(self, [row(id="t/a"), row(id="t/b")])
        rc, out = e.run("--only", "t/b")
        self.assertEqual(rc, 0, out)
        self.assertFalse(os.path.exists(os.path.join(e.out, "t/a")))
        self.assertTrue(os.path.exists(os.path.join(e.out, "t/b")))

    def test_serial_runs_alone(self):
        rows = [row(id=f"t/p{i}", cmd=f"SLEEP {i}") for i in range(3)]
        rows.insert(2, row(id="t/s", cmd="SLEEP s", serial=True))
        e = Env(self, rows)
        rc, out = e.run("-j", "4")
        self.assertEqual(rc, 0, out)
        with open(e.log) as f:
            ev = [ln for ln in f.read().splitlines()
                  if ln.startswith(("start", "end"))]
        i = ev.index("start t/s")
        self.assertEqual(ev[i + 1], "end t/s", ev)
        before = {x.split()[1] for x in ev[:i]}
        self.assertEqual(before, {"t/p0", "t/p1"}, ev)
        self.assertTrue(any(x == "start t/p2" for x in ev[i + 2:]), ev)


class TestIpBin(unittest.TestCase):
    """--ip-bin: every row runs that directory's ip, the helper is told."""

    def env_with_bin(self, rows, with_ip=True):
        e = Env(self, rows)
        d = os.path.join(e.tmp.name, "ip730")
        os.makedirs(d)
        if with_ip:
            p = os.path.join(d, "ip")
            with open(p, "w") as f:
                f.write(STUB_IP730)
            os.chmod(p, stat.S_IRWXU)
        return e, d

    def helper_env(self, e):
        with open(os.path.join(e.tmp.name, "nlcap-env.log")) as f:
            return set(f.read().splitlines())

    CASES: ClassVar[list[tuple]] = [
        # (description, use --ip-bin, pre step, want pre's ip, want
        # iproute2). The build sandbox has no ip on PATH, so only the
        # --ip-bin row's pre step calls one.
        ("positive: --ip-bin given; corner: the pre step's ip is it too",
         True, "ip -V > pre-ip.txt",
         "ip utility, iproute2-7.3.0-stub", "ip utility, iproute2-7.3.0-stub"),
        ("positive: no --ip-bin", False, "true",
         None, "ip utility, iproute2-stub"),
    ]

    def test_cases(self):
        for desc, use_bin, pre, want_pre_ip, want_ver in self.CASES:
            with self.subTest(desc):
                r = row(pre=[pre])
                e, d = self.env_with_bin([r])
                extra = ["--ip-bin", d] if use_bin else []
                rc, out = e.run("-j", "1", *extra)
                self.assertEqual(rc, 0, out)
                got = e.cmd_json("t/r")
                self.assertEqual(got["env"]["iproute2"], want_ver)
                self.assertEqual(self.helper_env(e),
                                 {"NLCAP_IP_BIN=" + (d if use_bin else "")})
                want_calls = ["ns-add", "ip730" if use_bin else "ip",
                              "write", "ns-del"]
                self.assertEqual(e.calls(), want_calls)
                if want_pre_ip:
                    with open(os.path.join(e.out, "t/r", "pre-ip.txt")) as f:
                        self.assertEqual(f.read().strip(), want_pre_ip)

    def test_no_ip_in_dir(self):
        e, d = self.env_with_bin([row()], with_ip=False)
        rc, out = e.run("--ip-bin", d)
        self.assertEqual(rc, 2, out)
        self.assertIn("NLCAP_ERR no ip in " + d, out)
        self.assertEqual(e.calls(), [], "no row may run")


class TestBpfObj(unittest.TestCase):
    """--bpf-obj: the object is linked at --bpf-link before any row runs."""

    CASES: ClassVar[list[tuple]] = [
        # (description, make the object, link exists before, want exit,
        # want the link to point at the object)
        ("positive: an object is linked", True, False, 0, True),
        ("corner: a stale link is replaced", True, True, 0, True),
        ("negative: no such object", False, False, 2, False),
    ]

    def test_cases(self):
        for desc, make, stale, want_rc, want_link in self.CASES:
            with self.subTest(desc):
                e = Env(self, [row()])
                obj = os.path.join(e.tmp.name, "lwt.o")
                if make:
                    with open(obj, "wb") as f:
                        f.write(b"\x7fELF")
                link = os.path.join(e.tmp.name, "run", "lwt_bpf.o")
                if stale:
                    os.makedirs(os.path.dirname(link))
                    os.symlink("/nonexistent", link)
                rc, out = e.run("--bpf-obj", obj, "--bpf-link", link)
                self.assertEqual(rc, want_rc, out)
                if not want_link:
                    self.assertIn("NLCAP_ERR no bpf object " + obj, out)
                    self.assertEqual(e.calls(), [], "no row may run")
                    continue
                self.assertEqual(os.readlink(link), obj)
                self.assertEqual(e.cmd_json("t/r")["env"]["lwt_bpf"], link)

    def test_no_flag_no_key(self):
        e = Env(self, [row()])
        rc, out = e.run()
        self.assertEqual(rc, 0, out)
        self.assertNotIn("lwt_bpf", e.cmd_json("t/r")["env"])


class TestKconfig(unittest.TestCase):
    """The guest's lwtunnel options land in env.kconfig, sorted."""

    CASES: ClassVar[list[tuple]] = [
        # (description, config text or None for no file, want kconfig)
        ("positive: both options set",
         "CONFIG_IPV6_RPL_LWTUNNEL=y\nCONFIG_NET=y\nCONFIG_IPV6_IOAM6_LWTUNNEL=y\n",
         ["CONFIG_IPV6_IOAM6_LWTUNNEL=y", "CONFIG_IPV6_RPL_LWTUNNEL=y"]),
        ("positive: an unset option is recorded too",
         "# CONFIG_IPV6_RPL_LWTUNNEL is not set\nCONFIG_LWTUNNEL=y\n",
         ["# CONFIG_IPV6_RPL_LWTUNNEL is not set", "CONFIG_LWTUNNEL=y"]),
        ("negative: no config file", None, None),
        ("boundary: no lwtunnel line", "CONFIG_NET=y\n", None),
    ]

    def test_cases(self):
        for desc, text, want in self.CASES:
            with self.subTest(desc):
                e = Env(self, [row()])
                path = os.path.join(e.tmp.name, "config.gz")
                if text is not None:
                    with gzip.open(path, "wt") as f:
                        f.write(text)
                rc, out = e.run("--kconfig", path)
                self.assertEqual(rc, 0, out)
                self.assertEqual(e.cmd_json("t/r")["env"].get("kconfig"), want)

    def test_corrupt_file(self):
        e = Env(self, [row()])
        path = os.path.join(e.tmp.name, "config.gz")
        with open(path, "wb") as f:
            f.write(b"not gzip")
        self.assertEqual(nlcap_batch.kconfig_lwt(path), [],
                         "corner: a file that is not gzip reads as none")


class TestSegments(unittest.TestCase):
    CASES: ClassVar[list[tuple]] = [
        ("positive: serial splits the run", "ppSp", ["pp", "S", "p"]),
        ("boundary: all parallel", "ppp", ["ppp"]),
        ("boundary: all serial", "SS", ["S", "S"]),
        ("corner: serial first and last", "SpS", ["S", "p", "S"]),
    ]

    def test_cases(self):
        for desc, shape, want in self.CASES:
            with self.subTest(desc):
                rows = [({"serial": c == "S"}, []) for c in shape]
                got = ["".join("S" if r[0]["serial"] else "p" for r in seg)
                       for seg in nlcap_batch.segments(rows)]
                self.assertEqual(got, want)


if __name__ == "__main__":
    unittest.main()
