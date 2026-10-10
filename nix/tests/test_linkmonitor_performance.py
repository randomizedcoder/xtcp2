"""Tables describe both rejected evidence and valid benchmark output."""
import unittest

from linkmonitor_performance import compatible, scenarios, validate_samples


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
