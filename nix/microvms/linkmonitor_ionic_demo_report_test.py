"""Table-driven tests for linkmonitor_ionic_demo_report."""

import math
import unittest

import linkmonitor_ionic_demo_report as report


class ParseMetricsTest(unittest.TestCase):
    """parse_metrics over the exposition shapes the monitor writes."""

    def test_parse_metrics(self):
        """Each row's text parses to its samples."""
        cases = [
            {
                "category": "positive",
                "description": "unlabelled sample",
                "text": "go_link_monitor_collection_healthy 1\n",
                "expected": [("go_link_monitor_collection_healthy", {}, 1.0)],
            },
            {
                "category": "positive",
                "description": "labelled sample with escaped quote",
                "text": 'm{interface="enp0s4",x="a\\"b"} 2.5e+09\n',
                "expected": [("m", {"interface": "enp0s4", "x": 'a\\"b'}, 2.5e9)],
            },
            {
                "category": "negative",
                "description": "comments, blanks and a non-numeric value",
                "text": '# HELP m x\n# TYPE m gauge\n\nm{a="b"} NaNish\n',
                "expected": [],
            },
            {
                "category": "boundary",
                "description": "empty input",
                "text": "",
                "expected": [],
            },
            {
                "category": "corner",
                "description": "NaN and +Inf parse as floats and are kept",
                "text": "a NaN\nb +Inf\n",
                "expected": [("a", {}, "nan"), ("b", {}, float("inf"))],
            },
        ]
        for case in cases:
            with self.subTest(category=case["category"], description=case["description"]):
                got = report.parse_metrics(case["text"])
                normalized = [(n, l, "nan" if math.isnan(v) else v) for n, l, v in got]
                self.assertEqual(normalized, case["expected"])


class FindTest(unittest.TestCase):
    """find matches the prefixed name and a label subset."""

    samples = [
        ("go_link_monitor_interface_channels", {"interface": "e0", "kind": "rx"}, 0.0),
        (
            "go_link_monitor_interface_channels",
            {"interface": "e0", "kind": "combined"},
            8.0,
        ),
        (
            "go_link_monitor_interface_channels",
            {"interface": "e1", "kind": "combined"},
            2.0,
        ),
    ]

    def test_find(self):
        """Each row's lookup returns its value."""
        cases = [
            {
                "category": "positive",
                "description": "interface and kind select one series",
                "name": "interface_channels",
                "labels": {"interface": "e0", "kind": "combined"},
                "expected": 8.0,
            },
            {
                "category": "positive",
                "description": "another interface",
                "name": "interface_channels",
                "labels": {"interface": "e1", "kind": "combined"},
                "expected": 2.0,
            },
            {
                "category": "negative",
                "description": "absent interface",
                "name": "interface_channels",
                "labels": {"interface": "e9"},
                "expected": None,
            },
            {
                "category": "negative",
                "description": "absent metric",
                "name": "interface_channels_max",
                "labels": {},
                "expected": None,
            },
            {
                "category": "corner",
                "description": "no labels returns the first series",
                "name": "interface_channels",
                "labels": {},
                "expected": 0.0,
            },
        ]
        for case in cases:
            with self.subTest(category=case["category"], description=case["description"]):
                self.assertEqual(
                    report.find(self.samples, case["name"], **case["labels"]),
                    case["expected"],
                )


class EthtoolTest(unittest.TestCase):
    """ethtool -S and -l parsing, and the per-queue use count."""

    stats_text = (
        "NIC statistics:\n"
        "     rx_packets: 300\n"
        "     tx_0_pkts: 25\n"
        "     tx_1_pkts: 0\n"
        "     rx_0_pkts: 7\n"
        "     rx_1_pkts: 3\n"
        "     rx_10_pkts: 1\n"
        "     tx_0_bytes: 2450\n"
        "     garbage line\n"
        "     odd: n/a\n"
    )

    def test_queues_used(self):
        """Each direction counts its queues with packets."""
        stats = report.parse_ethtool_stats(self.stats_text)
        cases = [
            {
                "category": "positive",
                "description": "rx queues",
                "direction": "rx",
                "expected": (3, 3),
            },
            {
                "category": "positive",
                "description": "tx with one idle queue",
                "direction": "tx",
                "expected": (1, 2),
            },
            {
                "category": "negative",
                "description": "unknown direction",
                "direction": "xx",
                "expected": (0, 0),
            },
        ]
        for case in cases:
            with self.subTest(category=case["category"], description=case["description"]):
                self.assertEqual(report.queues_used(stats, case["direction"]), case["expected"])

    def test_per_queue(self):
        """Each direction lists its queue packet counts in queue order."""
        stats = report.parse_ethtool_stats(self.stats_text)
        cases = [
            {
                "category": "positive",
                "description": "tx in order",
                "direction": "tx",
                "expected": [25, 0],
            },
            {
                "category": "corner",
                "description": "queue 10 sorts numerically after 1",
                "direction": "rx",
                "expected": [7, 3, 1],
            },
            {
                "category": "negative",
                "description": "unknown direction",
                "direction": "xx",
                "expected": [],
            },
        ]
        for case in cases:
            with self.subTest(category=case["category"], description=case["description"]):
                self.assertEqual(report.per_queue(stats, case["direction"]), case["expected"])

    def test_field(self):
        """Each key returns its value from `key: value` output."""
        text = (
            "driver: ionic\nfirmware-version: rocm-ernic-1.0\n"
            "\tPort: FIBRE\nbus-info: 0000:01:00.0\n"
        )
        cases = [
            {
                "category": "positive",
                "description": "ethtool -i key",
                "key": "firmware-version",
                "expected": "rocm-ernic-1.0",
            },
            {
                "category": "positive",
                "description": "indented ethtool key",
                "key": "Port",
                "expected": "FIBRE",
            },
            {
                "category": "corner",
                "description": "value containing colons",
                "key": "bus-info",
                "expected": "0000:01:00.0",
            },
            {"category": "negative", "description": "absent key", "key": "Speed", "expected": ""},
            {"category": "boundary", "description": "empty key", "key": "", "expected": ""},
        ]
        for case in cases:
            with self.subTest(category=case["category"], description=case["description"]):
                self.assertEqual(report.field(text, case["key"]), case["expected"])

    def test_parse_ethtool_stats(self):
        """Each key parses to its value or is skipped."""
        stats = report.parse_ethtool_stats(self.stats_text)
        cases = [
            {
                "category": "positive",
                "description": "aggregate counter",
                "key": "rx_packets",
                "expected": 300,
            },
            {
                "category": "corner",
                "description": "bytes are not queue packet counters",
                "key": "tx_0_bytes",
                "expected": 2450,
            },
            {
                "category": "negative",
                "description": "non-numeric value skipped",
                "key": "odd",
                "expected": None,
            },
            {
                "category": "negative",
                "description": "line without colon skipped",
                "key": "garbage line",
                "expected": None,
            },
        ]
        for case in cases:
            with self.subTest(category=case["category"], description=case["description"]):
                self.assertEqual(stats.get(case["key"]), case["expected"])

    def test_parse_ethtool_channels(self):
        """Each output gives its current and maximum combined."""
        cases = [
            {
                "category": "positive",
                "description": "ionic shape",
                "text": (
                    "Channel parameters for e0:\nPre-set maximums:\nRX:\t\tn/a\n"
                    "Combined:\t16\nCurrent hardware settings:\nRX:\t\tn/a\nCombined:\t8\n"
                ),
                "expected": (8, 16),
            },
            {
                "category": "negative",
                "description": "no combined lines",
                "text": "Channel parameters for e0:\nPre-set maximums:\nRX:\t1\n",
                "expected": (None, None),
            },
            {
                "category": "boundary",
                "description": "empty output",
                "text": "",
                "expected": (None, None),
            },
            {
                "category": "corner",
                "description": "combined n/a",
                "text": (
                    "Pre-set maximums:\nCombined:\tn/a\n"
                    "Current hardware settings:\nCombined:\tn/a\n"
                ),
                "expected": (None, None),
            },
        ]
        for case in cases:
            with self.subTest(category=case["category"], description=case["description"]):
                self.assertEqual(report.parse_ethtool_channels(case["text"]), case["expected"])


class FormatTest(unittest.TestCase):
    """number and bits cell formatting."""

    def test_format(self):
        """Each value formats to its cell text."""
        cases = [
            {
                "category": "positive",
                "description": "integer with separators",
                "fn": report.number,
                "value": 1234567.0,
                "expected": "1,234,567",
            },
            {
                "category": "positive",
                "description": "fraction",
                "fn": report.number,
                "value": 0.5,
                "expected": "0.5",
            },
            {
                "category": "negative",
                "description": "missing value",
                "fn": report.number,
                "value": None,
                "expected": "-",
            },
            {
                "category": "boundary",
                "description": "zero",
                "fn": report.number,
                "value": 0.0,
                "expected": "0",
            },
            {
                "category": "corner",
                "description": "NaN does not raise",
                "fn": report.number,
                "value": float("nan"),
                "expected": "nan",
            },
            {
                "category": "corner",
                "description": "+Inf does not raise",
                "fn": report.number,
                "value": float("inf"),
                "expected": "inf",
            },
            {
                "category": "positive",
                "description": "400G",
                "fn": report.bits,
                "value": 4e11,
                "expected": "400 Gb/s",
            },
            {
                "category": "negative",
                "description": "missing speed",
                "fn": report.bits,
                "value": None,
                "expected": "-",
            },
        ]
        for case in cases:
            with self.subTest(category=case["category"], description=case["description"]):
                self.assertEqual(case["fn"](case["value"]), case["expected"])


if __name__ == "__main__":
    unittest.main()
