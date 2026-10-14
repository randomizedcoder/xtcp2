"""Write report.md for demo-linkmonitor-ionic-profiles.

usage: linkmonitor_ionic_demo_report.py OUT SCENARIO...

Each OUT/SCENARIO/guest holds one guest's captures: the monitor's /metrics
and /metrics-all after traffic, /metrics-all before it, and the driver's own
view (ethtool, ip -s link, the RDMA hw_counters files). The report puts the
profiles side by side and shows what reached Prometheus for each.
"""

import math
import re
import sys
from pathlib import Path

SAMPLE = re.compile(r"^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{(.*)\})?\s+(\S+)")
LABEL = re.compile(r'([a-zA-Z_][a-zA-Z0-9_]*)="((?:[^"\\]|\\.)*)"')
QUEUE_STAT = re.compile(r"^(rx|tx)_(\d+)_pkts$")
PREFIX = "go_link_monitor_"


def parse_metrics(text):
    """Parse Prometheus text exposition into (name, labels, value) tuples.

    Comments, blank lines and samples whose value is not a float are skipped.
    """
    samples = []
    for line in text.splitlines():
        if not line or line.startswith("#"):
            continue
        match = SAMPLE.match(line)
        if not match:
            continue
        try:
            value = float(match.group(4))
        except ValueError:
            continue
        labels = dict(LABEL.findall(match.group(3) or ""))
        samples.append((match.group(1), labels, value))
    return samples


def find(samples, name, **labels):
    """Return the first value of PREFIX+name whose labels include labels."""
    for sample_name, sample_labels, value in samples:
        if sample_name != PREFIX + name:
            continue
        if all(sample_labels.get(k) == v for k, v in labels.items()):
            return value
    return None


def parse_ethtool_stats(text):
    """Parse `ethtool -S` output into {statistic: int}."""
    stats = {}
    for line in text.splitlines():
        key, sep, value = line.strip().partition(":")
        if not sep:
            continue
        try:
            stats[key.strip()] = int(value.strip())
        except ValueError:
            continue
    return stats


def queues_used(stats, direction):
    """Return (queues with packets, queues listed) for rx or tx."""
    used = listed = 0
    for key, value in stats.items():
        match = QUEUE_STAT.match(key)
        if not match or match.group(1) != direction:
            continue
        listed += 1
        if value > 0:
            used += 1
    return used, listed


def parse_ethtool_channels(text):
    """Return (current combined, maximum combined) from `ethtool -l`."""
    maximum = current = None
    section = None
    for line in text.splitlines():
        lower = line.strip().lower()
        if lower.startswith("pre-set maximums"):
            section = "max"
        elif lower.startswith("current hardware settings"):
            section = "current"
        elif lower.startswith("combined:") and section:
            value = lower.split(":", 1)[1].strip()
            count = int(value) if value.isdigit() else None
            if section == "max":
                maximum = count
            else:
                current = count
    return current, maximum


def per_queue(stats, direction):
    """Return the rx_N_pkts or tx_N_pkts values in queue order."""
    queues = {}
    for key, value in stats.items():
        match = QUEUE_STAT.match(key)
        if match and match.group(1) == direction:
            queues[int(match.group(2))] = value
    return [queues[index] for index in sorted(queues)]


def field(text, key):
    """Return the value of a `key: value` line, or an empty string."""
    for line in text.splitlines():
        name, sep, value = line.strip().partition(":")
        if sep and name == key:
            return value.strip()
    return ""


def number(value):
    """Format a sample value for a table cell."""
    if value is None:
        return "-"
    if math.isnan(value) or math.isinf(value):
        return str(value)
    if value == int(value):
        return f"{int(value):,}"
    return f"{value:g}"


def bits(value):
    """Format bits per second as Gb/s."""
    if value is None:
        return "-"
    return f"{value / 1e9:g} Gb/s"


def read(path):
    """Return the file's text, or an empty string if it is missing."""
    try:
        return path.read_text(encoding="utf-8", errors="replace")
    except FileNotFoundError:
        return ""


class Capture:
    """One scenario's captures."""

    def __init__(self, root, scenario):
        self.scenario = scenario
        self.guest = root / scenario / "guest"
        self.args = read(root / scenario / "ernic-args.txt").strip()
        self.netdev = self.text("netdev.txt").strip()
        self.rdmadev = self.text("rdmadev.txt").strip()
        self.all = parse_metrics(self.text("metrics-all.txt"))
        self.before = parse_metrics(self.text("metrics-all-before.txt"))

    def text(self, name):
        """Return one guest capture file's text."""
        return read(self.guest / name)

    def core(self):
        """Return the parsed summary /metrics scrape."""
        return parse_metrics(self.text("metrics.txt"))

    def ethtool(self):
        """Return the parsed `ethtool -S`."""
        return parse_ethtool_stats(self.text("ethtool-S.txt"))

    def iface(self, name, samples=None, **labels):
        """Return name's value for this capture's netdev."""
        if samples is None:
            samples = self.all
        return find(samples, name, interface=self.netdev, **labels)

    def delta(self, name):
        """Return the change in an interface counter across the traffic."""
        after = self.iface(name)
        before = self.iface(name, self.before)
        if after is None or before is None:
            return None
        return after - before

    def hw_counters(self):
        """Return {counter: value} for the catch-all RDMA series."""
        return {
            labels.get("counter", ""): value
            for name, labels, value in self.all
            if name == PREFIX + "rdma_hw_counter"
        }

    def excerpt(self):
        """Return the /metrics-all lines that describe this NIC."""
        wanted = ("interface_", "ethtool_statistic", "rdma_", "infiniband_")
        lines = []
        for line in self.text("metrics-all.txt").splitlines():
            if line.startswith("#") or not line.startswith(PREFIX):
                continue
            if not line[len(PREFIX) :].startswith(wanted):
                continue
            if f'"{self.netdev}"' not in line and f'"{self.rdmadev}"' not in line:
                continue
            if line.startswith(PREFIX + "ethtool_statistic") and line.endswith(" 0"):
                continue
            lines.append(line)
        return lines


def queue_cell(capture, direction):
    """Return 'used of listed: v0 / v1 / ...' for one direction."""
    stats = capture.ethtool()
    used, listed = queues_used(stats, direction)
    values = " / ".join(number(v) for v in per_queue(stats, direction))
    return f"{used} of {listed}: {values}"


def hw_counter_cell(capture):
    """Return 'series (nonzero)' for the catch-all RDMA counters."""
    counters = capture.hw_counters()
    nonzero = sum(1 for value in counters.values() if value)
    return f"{len(counters)} ({nonzero})"


def probe_summary(text):
    """Return (ok, failed, skipped) row counts from ernic-rdma-errors output."""
    counts = {"ok": 0, "FAIL": 0, "skip": 0}
    for line in text.splitlines():
        word = line.split(" ", 1)[0]
        if word in counts:
            counts[word] += 1
    return counts["ok"], counts["FAIL"], counts["skip"]


def probe_cell(capture):
    """Return the ernic-rdma-errors summary, or '-' when it did not run."""
    text = capture.text("rdma-errors.txt")
    if not text:
        return "-"
    ok, failed, skipped = probe_summary(text)
    cell = f"{ok} ok, {failed} failed"
    if skipped:
        cell += f", {skipped} skipped"
    return cell


def comparison():
    """Return the side-by-side table rows: (label, cell function)."""
    rows = [
        ("rocm-ernic arguments", lambda c: f"`{c.args}`"),
        ("guest vCPUs", lambda c: c.text("nproc.txt").strip()),
        (
            "ethtool -i firmware-version",
            lambda c: field(c.text("ethtool-i.txt"), "firmware-version") or "-",
        ),
        ("ethtool Port", lambda c: field(c.text("ethtool.txt"), "Port") or "-"),
        (
            "/metrics-all interface_speed_bits_per_second",
            lambda c: bits(c.iface("interface_speed_bits_per_second")),
        ),
        (
            "ethtool -l combined (current / max)",
            lambda c: " / ".join(
                number(v) for v in parse_ethtool_channels(c.text("ethtool-l.txt"))
            ),
        ),
        (
            "/metrics-all interface_channels{kind=combined} / _max",
            lambda c: f"{number(c.iface('interface_channels', kind='combined'))} / "
            f"{number(c.iface('interface_channels_max', kind='combined'))}",
        ),
        ("TX queues with packets: tx_N_pkts", lambda c: queue_cell(c, "tx")),
        ("RX queues with packets: rx_N_pkts", lambda c: queue_cell(c, "rx")),
    ]
    for name in (
        "interface_receive_packets_total",
        "interface_transmit_packets_total",
        "interface_receive_dropped_total",
        "interface_transmit_dropped_total",
        "interface_receive_over_errors_total",
    ):
        rows.append((f"/metrics-all {name}, +during traffic", lambda c, n=name: number(c.delta(n))))
    rows += [
        ("ernic-rdma-errors rows", probe_cell),
        ("/metrics-all rdma_hw_counter series (nonzero)", hw_counter_cell),
        (
            "/metrics-all rdma_hw_counter{counter=total_bytes_sent}",
            lambda c: number(c.hw_counters().get("total_bytes_sent")),
        ),
        (
            "/metrics collection_healthy",
            lambda c: number(find(c.core(), "collection_healthy")),
        ),
    ]
    return rows


def render(captures):
    """Return report.md's text."""
    out = ["# go-link-monitor on the rocm-ernic virtual ionic NIC, per profile", ""]
    out.append(
        "Each column is one guest: the in-tree ionic and ionic_rdma drivers bound "
        "to rocm-ernic with the given `--nic-profile`, a TAP peer, ping and TCP "
        "SYN traffic from every vCPU, an RDMA RC ping-pong, and, where rocm-ernic "
        "provides it, ernic-rdma-errors making each RDMA error happen once. The "
        "values are what go-link-monitor exported; the driver's own view "
        "(ethtool, ip -s link, sysfs) is beside each guest's scrapes."
    )
    out.append("")
    header = "| | " + " | ".join(c.scenario for c in captures) + " |"
    out += [header, "|---" * (len(captures) + 1) + "|"]
    for label, cell in comparison():
        out.append(f"| {label} | " + " | ".join(cell(c) for c in captures) + " |")
    out.append("")
    for capture in captures:
        out += [f"## {capture.scenario}", ""]
        out.append(
            f"netdev `{capture.netdev}`, RDMA device `{capture.rdmadev}`, "
            f"{capture.text('nproc.txt').strip()} vCPUs, `{capture.args}`."
        )
        out.append("")
        nonzero = {k: v for k, v in capture.hw_counters().items() if v}
        if nonzero:
            out.append(
                "Nonzero RDMA hw_counters: "
                + ", ".join(f"`{k}` {number(v)}" for k, v in sorted(nonzero.items()))
                + "."
            )
            out.append("")
        probe = capture.text("rdma-errors.txt")
        if probe:
            out += ["<details><summary>ernic-rdma-errors</summary>", "", "```"]
            out += [line for line in probe.splitlines() if line]
            out += ["```", "", "</details>", ""]
        out += ["<details><summary>/metrics (summary)</summary>", "", "```"]
        out += [line for line in capture.text("metrics.txt").splitlines() if line]
        out += ["```", "", "</details>", ""]
        out += [
            "<details><summary>/metrics-all, this NIC's series "
            "(zero ethtool statistics omitted)</summary>",
            "",
            "```",
        ]
        out += capture.excerpt()
        out += ["```", "", "</details>", ""]
    return "\n".join(out)


def main(argv):
    """Write OUT/report.md for the named scenarios."""
    if len(argv) < 3:
        print(__doc__.strip().splitlines()[2], file=sys.stderr)
        return 2
    root = Path(argv[1])
    captures = [Capture(root, scenario) for scenario in argv[2:]]
    (root / "report.md").write_text(render(captures) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
