"""Fresh, sequential performance execution; Nix caches binaries, never results."""

import argparse
import collections
import json
import os
from pathlib import Path
import platform
import re
import resource
import shutil
import subprocess
import tempfile
import time


BENCH = re.compile(r"^(Benchmark\S+)\s+\d+\s+.*\bns/op\b", re.MULTILINE)


def validate_samples(text, repetitions, expected=None):
    counts = collections.Counter(BENCH.findall(text))
    if not counts or any(count != repetitions for count in counts.values()):
        raise ValueError("missing benchmarks or incomplete repetitions")
    if "FAIL" in text or not re.search(r"^PASS$", text, re.MULTILINE):
        raise ValueError("benchmark executable did not pass")
    if expected is not None and set(counts) != set(expected):
        raise ValueError("benchmark names differ from warmup")
    return dict(counts)


def validate_profile(path, minimum_bytes=64):
    """Reject missing or truncated pprof output before accepting a soak."""
    if not path.is_file() or path.stat().st_size < minimum_bytes:
        raise ValueError(f"missing or truncated profile: {path}")


def scenarios(warmup=5000, measure=30000):
    base = dict(Ports=32, Fields=64, Rate=1000, Scrapers=1, Burst=0, Mode="",
                DelayMS=0, WarmupMS=warmup, MeasureMS=measure)
    cases = {"reference": base}
    for key, values in {"Ports": [2, 8, 128, 256], "Fields": [0, 1024, 8192],
                        "Rate": [0, 1, 10000], "Scrapers": [0, 4, 10],
                        "DelayMS": [1, 10, 100, 5000], "Burst": [4096, 8192]}.items():
        for value in values:
            cases[f"{key}-{value}"] = dict(base, **{key: value})
    cases["combined"] = dict(base, Ports=256, Fields=1024, Scrapers=10)
    for mode in ("one-stuck", "all-stuck", "failed-resync", "schema-churn", "rename", "hotplug", "slow-client", "disconnected", "cadence"):
        cases[mode] = dict(base, Mode=mode)
    return cases


def compatible(old, new):
    for key in ("schema", "scenarios", "repetitions", "benchtime", "gomaxprocs", "benchmarks", "seed", "go"):
        if old.get(key) != new.get(key):
            raise ValueError(f"incompatible baseline: {key}")


def process_sample(pid):
    """Capture bounded process-resource evidence while a child is running."""
    try:
        statm = Path(f"/proc/{pid}/statm").read_text().split()
        status = Path(f"/proc/{pid}/status").read_text()
        rss = next(int(line.split()[1]) for line in status.splitlines()
                   if line.startswith("VmRSS:"))
        return {"t": time.monotonic(), "rss_kib": rss,
                "pages": int(statm[1]),
                "fds": len(list(Path(f"/proc/{pid}/fd").iterdir())),
                "threads": len(list(Path(f"/proc/{pid}/task").iterdir()))}
    except (FileNotFoundError, IndexError, StopIteration, ValueError):
        return None


def execute(command, output, env, timeout, samples=None):
    before = resource.getrusage(resource.RUSAGE_CHILDREN)
    started = time.monotonic()
    metadata = dict(command=command, status="incomplete", load_before=os.getloadavg())
    sidecar = output.with_suffix(".command.json")
    sidecar.write_text(json.dumps(metadata, indent=2))
    with output.open("w") as log:
        child = subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT, env=env)
        started_sample = time.monotonic()
        while child.poll() is None:
            if samples is not None and time.monotonic() - started_sample >= 1:
                sample = process_sample(child.pid)
                if sample is not None:
                    samples.append(sample)
                started_sample = time.monotonic()
            if time.monotonic() - started > timeout:
                child.kill()
                child.wait()
                raise subprocess.TimeoutExpired(command, timeout)
            time.sleep(0.05)
        if child.returncode:
            raise subprocess.CalledProcessError(child.returncode, command)
    after = resource.getrusage(resource.RUSAGE_CHILDREN)
    metadata.update(status="complete", load_after=os.getloadavg(), wall_seconds=time.monotonic() - started,
                user_seconds=after.ru_utime - before.ru_utime,
                system_seconds=after.ru_stime - before.ru_stime,
                cumulative_children_peak_rss_kib=after.ru_maxrss)
    sidecar.write_text(json.dumps(metadata, indent=2))
    return metadata


def arguments():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--baseline", type=Path)
    parser.add_argument("--gomaxprocs", type=int, default=min(4, len(os.sched_getaffinity(0))))
    parser.add_argument("--smoke", action="store_true", help="short incomplete measurements, never a baseline")
    parser.add_argument("--soak", action="store_true", help="run the ten-minute warmed profile soak")
    args = parser.parse_args()
    if args.gomaxprocs < 1:
        parser.error("gomaxprocs must be positive")
    return args


def run(args, artifact, output):
    repetitions = 1 if args.smoke else 10
    manifest = dict(schema=1, repetitions=repetitions, benchtime="1x" if args.smoke else "1s",
                    gomaxprocs=args.gomaxprocs, scenarios=scenarios(10, 50) if args.smoke else scenarios(),
                    benchmarks="Performance|StatisticSchema|StatisticCached|HostCollection|RDMA|EventDelivery",
                    seed=0, source=(artifact / "source.txt").read_text().strip(),
                    artifact=str(artifact), go=(artifact / "go-version.txt").read_text().strip(),
                    benchstat=shutil.which("benchstat"),
                    variants={"core": "CGO_ENABLED=0; monitor_bench"},
                    runtime_settings={"GOGC": "100", "GOMEMLIMIT": "off", "GODEBUG": ""},
                    kernel=platform.uname()._asdict(), affinity=sorted(os.sched_getaffinity(0)),
                    cpuinfo=Path("/proc/cpuinfo").read_text(), status="incomplete")
    (output / "manifest.json").write_text(json.dumps(manifest, indent=2))
    if args.baseline:
        old = json.loads((args.baseline / "manifest.json").read_text())
        if old.get("status") != "complete":
            raise ValueError("baseline is incomplete")
        compatible(old, manifest)
    env = dict(os.environ, GOMAXPROCS=str(args.gomaxprocs), **manifest["runtime_settings"])
    commands = []
    if args.soak:
        # Large schemas are scraped at the production-like 15-second cadence;
        # the short matrix retains continuous-scrape stress without allowing a
        # ten-minute run to manufacture unbounded response bytes.
        # The 256x1024 case remains a bounded diagnostic scenario in the
        # matrix. The ten-minute profile run uses 256x64 so its RSS/thread
        # plateau is measurable on the shared host rather than an OOM test.
        soak = dict(Ports=256, Fields=64, Rate=1000, Scrapers=1, Burst=8192,
                    Mode="stream-cadence", DelayMS=0, WarmupMS=60000, MeasureMS=600000)
        scoped = dict(env, LINKMONITOR_BENCH_SCENARIO=json.dumps(soak))
        reports = {}
        for variant in ("core",):
            profile_dir = output / variant
            profile_dir.mkdir()
            samples = []
            binary = str(artifact / "bin" / f"{variant}.test")
            command = [binary, "-test.run=^TestPerformanceScenario$", "-test.v",
                       "-test.timeout=20m", "-test.cpuprofile", str(profile_dir / "cpu.pprof"),
                       "-test.memprofile", str(profile_dir / "heap.pprof"),
                       "-test.blockprofile", str(profile_dir / "block.pprof"),
                       "-test.mutexprofile", str(profile_dir / "mutex.pprof")]
            log = output / f"{variant}-soak.txt"
            commands.append(execute(command, log, scoped, 1500, samples))
            for profile in ("cpu.pprof", "heap.pprof", "block.pprof", "mutex.pprof"):
                validate_profile(profile_dir / profile)
            records = re.findall(r"^PERFORMANCE_RESULT (.+)$", log.read_text(), re.MULTILINE)
            if len(records) != 1:
                raise ValueError("missing or ambiguous soak result")
            reports[variant] = {"scenario": soak, "result": json.loads(records[0]),
                                "resource_samples": samples}
        (output / "soak.json").write_text(json.dumps(reports, indent=2))
        manifest["soak"] = soak
        manifest["status"] = "complete"
        (output / "commands.json").write_text(json.dumps(commands, indent=2))
        (output / "manifest.json").write_text(json.dumps(manifest, indent=2))
        return
    for variant in ("core",):
        binary = str(artifact / "bin" / f"{variant}.test")
        common = [binary, "-test.run=^$", f"-test.bench=^Benchmark({manifest['benchmarks']})", "-test.benchmem"]
        commands.append(execute(common + ["-test.benchtime=1x"], output / f"{variant}-warmup.txt", env, 300))
        raw = output / f"{variant}.txt"
        commands.append(execute(common + [f"-test.count={repetitions}", f"-test.benchtime={manifest['benchtime']}", "-test.timeout=60m"], raw, env, 3600))
        expected = validate_samples((output / f"{variant}-warmup.txt").read_text(), 1)
        validate_samples(raw.read_text(), repetitions, expected)
        compare = [str(args.baseline / f"{variant}.txt")] if args.baseline else []
        commands.append(execute(["benchstat", *compare, str(raw)], output / f"{variant}-benchstat.txt", env, 60))
    reports = {}
    for variant in ("core",):
        for name, scenario in manifest["scenarios"].items():
            log = output / f"{variant}-{name}.txt"
            scoped = dict(env, LINKMONITOR_BENCH_SCENARIO=json.dumps(scenario))
            commands.append(execute([str(artifact / "bin" / f"{variant}.test"), "-test.run=^TestPerformanceScenario$", "-test.v", "-test.timeout=10m"], log, scoped, 620))
            records = re.findall(r"^PERFORMANCE_RESULT (.+)$", log.read_text(), re.MULTILINE)
            if len(records) != 1:
                raise ValueError("missing or ambiguous scenario result")
            reports[f"{variant}-{name}"] = json.loads(records[0])
    (output / "commands.json").write_text(json.dumps(commands, indent=2))
    (output / "scenarios.json").write_text(json.dumps(reports, indent=2))
    manifest["capacity_limited_scenarios"] = [name for name, report in reports.items()
                                             if report["warmup"]["outcome"] == "capacity-limited"]
    manifest["scenario_comparison"] = "Only fully populated, equal-size observations support steady-state comparisons; benchstat compares microbenchmarks only."
    manifest["status"] = "smoke-only" if args.smoke else "complete"
    (output / "manifest.json").write_text(json.dumps(manifest, indent=2))


def main():
    args = arguments()
    output = args.output or Path(tempfile.mkdtemp(prefix="linkmonitor-baseline-"))
    if args.output:
        output.mkdir(parents=True, exist_ok=False)
    print(f"Evidence: {output}", flush=True)
    run(args, Path(os.environ["LINKMONITOR_BENCH_ARTIFACT"]), output)


if __name__ == "__main__":
    main()
