"""Start-up cost of go-link-monitor builds (GO-NATIVE-RDMA R4-T03).

usage: bench-linkmonitor-startup --output DIR [--count N] LABEL=BINARY...

Each run starts BINARY with an empty environment, -metrics-addr on a free
loopback port, -settle=0, and a new -baseline-file and admin socket, polls
/metrics, and records the time from exec to the first reply in which the
baseline is acquired and collection healthy, and the resident set at that
moment. A second run times `BINARY -version` to exit, which is the dynamic
loader and Go runtime start alone. Runs alternate between the binaries so host drift spreads evenly.
DIR/LABEL.txt is benchmark format, and DIR/startup-benchstat.txt compares
the labels against the first one. The monitor's own logs name host
interfaces, so they stay in a temporary directory, named only on failure.
"""

import argparse
import http.client
import os
import platform
import socket
import subprocess
import sys
import tempfile
import time
from pathlib import Path

READY_TIMEOUT = 30.0
READY_METRICS = (b"\ngo_link_monitor_baseline_ready 1\n", b"\ngo_link_monitor_collection_healthy 1\n")


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def rss_bytes(pid):
    for line in Path(f"/proc/{pid}/status").read_text().splitlines():
        if line.startswith("VmRSS:"):
            return int(line.split()[1]) * 1024
    raise RuntimeError(f"no VmRSS for {pid}")


def scraped(port):
    connection = http.client.HTTPConnection("127.0.0.1", port, timeout=1)
    try:
        connection.request("GET", "/metrics")
        response = connection.getresponse()
        body = b"\n" + response.read()
        return response.status == 200 and all(metric in body for metric in READY_METRICS)
    except OSError:
        return False
    finally:
        connection.close()


def ready_run(binary, log, state):
    port = free_port()
    started = time.monotonic_ns()
    process = subprocess.Popen([binary, "-metrics-addr", f"127.0.0.1:{port}", "-settle=0",
                                "-baseline-file", str(state / "baseline.json"),
                                "-admin.address", str(state / "admin.sock")], env={},
                               stdout=log, stderr=log)
    try:
        while not scraped(port):
            if process.poll() is not None:
                raise RuntimeError(f"{binary} exited {process.returncode} before its first scrape")
            if time.monotonic_ns() - started > READY_TIMEOUT * 1e9:
                raise RuntimeError(f"{binary} not ready after {READY_TIMEOUT}s")
            time.sleep(0.001)
        elapsed = time.monotonic_ns() - started
        rss = rss_bytes(process.pid)
        if process.poll() is not None:
            raise RuntimeError(f"{binary} exited {process.returncode} once ready")
        return elapsed, rss
    finally:
        process.terminate()
        process.wait(timeout=10)


def version_run(binary):
    started = time.monotonic_ns()
    subprocess.run([binary, "-version"], env={}, check=True, stdout=subprocess.DEVNULL)
    return time.monotonic_ns() - started


def cpu_model():
    for line in Path("/proc/cpuinfo").read_text().splitlines():
        if line.startswith("model name"):
            return line.split(":", 1)[1].strip()
    return "unknown"


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--count", type=int, default=20)
    parser.add_argument("binaries", nargs="+", metavar="LABEL=BINARY")
    args = parser.parse_args()
    if args.count < 1:
        parser.error("--count must be at least 1")
    builds = []
    for item in args.binaries:
        label, sep, binary = item.partition("=")
        if not sep or not label or not os.access(binary, os.X_OK):
            parser.error(f"{item}: want LABEL=BINARY with an executable BINARY")
        builds.append((label, binary))
    if len({label for label, _ in builds}) != len(builds):
        parser.error("labels must be unique")

    args.output.mkdir(parents=True, exist_ok=True)
    goarch = {"x86_64": "amd64", "aarch64": "arm64"}.get(platform.machine(), platform.machine())
    header = f"goos: linux\ngoarch: {goarch}\ncpu: {cpu_model()}\n"
    results = {label: [] for label, _ in builds}
    logs = Path(tempfile.mkdtemp(prefix="bench-linkmonitor-startup-"))
    try:
        for _ in range(args.count):
            for label, binary in builds:
                with open(logs / f"{label}.log", "w") as log, tempfile.TemporaryDirectory() as state:
                    ready, rss = ready_run(binary, log, Path(state))
                version = version_run(binary)
                results[label].append(f"BenchmarkStartup/metrics-ready 1 {ready} ns/op {rss} rss-B\n"
                                      f"BenchmarkStartup/version 1 {version} ns/op\n")
    except Exception:
        print(f"bench-linkmonitor-startup: monitor logs in {logs}", file=sys.stderr)
        raise
    for log in logs.iterdir():
        log.unlink()
    logs.rmdir()
    manifest = [f"count={args.count}", f"kernel={platform.release()}",
                f"load_after={' '.join(f'{x:.2f}' for x in os.getloadavg())}"]
    for label, binary in builds:
        (args.output / f"{label}.txt").write_text(header + "".join(results[label]))
        manifest.append(f"binary_{label}={binary} size={os.path.getsize(binary)}")
    (args.output / "manifest.txt").write_text("\n".join(manifest) + "\n")
    with open(args.output / "startup-benchstat.txt", "w") as out:
        subprocess.run(["benchstat"] + [f"{label}={args.output / (label + '.txt')}" for label, _ in builds],
                       check=True, stdout=out)
    sys.stdout.write((args.output / "startup-benchstat.txt").read_text())


if __name__ == "__main__":
    main()
