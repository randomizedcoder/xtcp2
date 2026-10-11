"""Tables describe both rejected evidence and valid benchmark output."""
import unittest

from pathlib import Path
import tempfile

from linkmonitor_performance import compatible, scenarios, validate_profile, validate_samples
from linkmonitor_http import write_urls


class PerformanceReportTests(unittest.TestCase):
    def test_repetitions(self):
        row = "BenchmarkExample-4 10 100 ns/op 0 B/op 0 allocs/op\n"
        for category, description, text, count, accepted in [
            ("positive", "ten complete repetitions accepted", row * 10 + "PASS\n", 10, True),
            ("negative", "nine repetitions rejected", row * 9 + "PASS\n", 10, False),
            ("boundary", "empty output rejected", "", 10, False),
            ("corner", "failure after samples rejected", row * 10 + "FAIL\n", 10, False),
        ]:
            with self.subTest(category=category, description=description, expected=accepted):
                if accepted:
                    self.assertEqual(validate_samples(text, count), {"BenchmarkExample-4": count})
                else:
                    with self.assertRaises(ValueError):
                        validate_samples(text, count)

    def test_compatibility(self):
        compatible({"schema": 1}, {"schema": 1})
        with self.assertRaisesRegex(ValueError, "gomaxprocs"):
            compatible({"gomaxprocs": 4}, {"gomaxprocs": 8})

    def test_missing_benchmark(self):
        with self.assertRaisesRegex(ValueError, "names differ"):
            validate_samples("BenchmarkOne-4 1 1 ns/op\nPASS\n", 1, {"BenchmarkTwo-4": 1})

    def test_bounded_matrix(self):
        cases = scenarios()
        self.assertEqual(cases["reference"]["Ports"], 32)
        self.assertEqual(cases["combined"]["Fields"], 1024)
        self.assertLess(len(cases), 40)
        self.assertTrue(all(c["Ports"] * c["Fields"] <= 256 * 1024 for c in cases.values()))

    def test_profile_artifact_table(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "profile.pprof"
            for category, description, payload, accepted in [
                ("positive", "complete profile", b"x" * 64, True),
                ("negative", "missing profile", None, False),
                ("boundary", "one byte below minimum", b"x" * 63, False),
                ("corner", "empty profile", b"", False),
            ]:
                with self.subTest(category=category, description=description, expected=accepted):
                    if payload is None:
                        if path.exists():
                            path.unlink()
                    else:
                        path.write_bytes(payload)
                    if accepted:
                        validate_profile(path)
                    else:
                        with self.assertRaises(ValueError):
                            validate_profile(path)

    def test_http_url_mix_table(self):
        for category, description, base, weights, accepted, expected in [
            ("positive", "default metrics and health mix", "http://127.0.0.1:9088", (90, 5, 5), True, 100),
            ("negative", "unsupported scheme rejected", "ftp://127.0.0.1", (90, 5, 5), False, 0),
            ("boundary", "zero health control traffic accepted", "http://localhost", (100, 0, 0), True, 100),
            ("corner", "weights above total rejected", "http://localhost", (90, 10, 1), False, 0),
            ("positive", "core, health and full-exposition mix", "http://localhost", (89, 5, 5, 1), True, 100),
            ("boundary", "full exposition only", "http://localhost", (0, 0, 0, 100), True, 100),
            ("negative", "negative full-exposition weight rejected", "http://localhost", (91, 5, 5, -1), False, 0),
            ("corner", "full exposition pushes total above 100", "http://localhost", (90, 5, 5, 1), False, 0),
        ]:
            with self.subTest(category=category, description=description, expected=accepted):
                with tempfile.TemporaryDirectory() as directory:
                    target = Path(directory) / "urls.txt"
                    if accepted:
                        result = write_urls(base, target, target, *weights)
                        self.assertEqual(sum(result.values()), expected)
                        self.assertEqual(len(target.read_text().splitlines()), 100)
                    else:
                        with self.assertRaises(ValueError):
                            write_urls(base, target, target, *weights)
