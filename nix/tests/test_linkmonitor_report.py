"""Negative cases prevent documentation/replay gates from silently succeeding."""

import tempfile
from pathlib import Path
import unittest

from linkmonitor_report import check_links, check_replay, check_tracker


class ReportTests(unittest.TestCase):
    def test_tracker(self):
        plan = "#### P01-T01\n"
        status = ("1 of 1 implementation tasks\n"
                  "| [x] | [P01-T01](plan) | work | done | proof |\n"
                  "| P01 | phase | done | 1/1 | proof |\n")
        cases = [
            ("consistent task and phase accepted", status, True),
            ("unchecked completed task rejected", status.replace("[x]", "[ ]"), False),
            ("missing task rejected", status.replace("P01-T01", "P01-T02"), False),
            ("stale phase total rejected", status.replace("1/1", "0/1"), False),
            ("stale headline rejected", status.replace("1 of 1", "0 of 1"), False),
        ]
        for description, text, valid in cases:
            with self.subTest(description=description, expected_success=valid):
                if valid:
                    check_tracker(plan, text)
                else:
                    with self.assertRaises(ValueError):
                        check_tracker(plan, text)

    def test_links(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "doc.md"
            for description, text, valid in [
                ("local anchor resolves", "# Target\n[link](#target)\n", True),
                ("missing anchor fails", "[link](#missing)\n", False),
                ("missing file fails", "[link](missing.md)\n", False),
                ("trailing spaces fail", "text \n", False),
            ]:
                with self.subTest(description=description, expected_success=valid):
                    path.write_text(text)
                    if valid:
                        check_links(path)
                    else:
                        with self.assertRaises(ValueError):
                            check_links(path)

    def test_replay(self):
        roots = ["TestLinkStateKernelFixtures", "TestLinkStateRouteKernelFixtures"]
        events = [{"Action": "pass", "Test": root} for root in roots]
        events += [{"Action": "pass", "Test": f"{root}/kernel/case{i}"}
                   for root in roots for i in range(20)]
        for description, rows, valid in [
            ("40 outcomes and both suites pass", events, True),
            ("empty output fails", [], False),
            ("missing leaf fails", events[:-1], False),
            ("skip fails despite passing leaves", events + [{"Action": "skip"}], False),
            ("failure fails despite passing leaves", events + [{"Action": "fail"}], False),
            ("duplicate leaf fails", events + [events[-1]], False),
        ]:
            with self.subTest(description=description, expected_success=valid):
                if valid:
                    check_replay(rows)
                else:
                    with self.assertRaises(ValueError):
                        check_replay(rows)
