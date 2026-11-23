"""Negative cases prevent the docs/netlink gates from silently succeeding."""

import tempfile
from pathlib import Path
import unittest

from goip_docs import check_docs, check_links, check_upstream


def tree(tmp, files):
    """Write {name: text} into tmp and return the docs/netlink base."""
    base = tmp / "docs" / "netlink"
    for name, text in files.items():
        path = base / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)
    return base


GOOD = {
    "README.md": (
        "# index\n"
        "\n"
        "- [plan](plan.md) names [the bond bug](upstream/note.md).\n"
    ),
    "plan.md": "# plan\n\nSee [the index](README.md#index) and the rows.\n",
    "upstream/README.md": (
        "# upstream index\n"
        "\n"
        "- note.md, a finding\n"
        "- 0000-cover.patch, the series\n"
    ),
    "upstream/note.md": "# note\n\nA finding, recorded.\n",
    "upstream/0000-cover.patch": "From: a <b>\n",
}


class LinkTests(unittest.TestCase):
    def test_good_document_passes(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = tree(Path(tmp), GOOD) / "plan.md"
            check_links(path)  # no exception

    def test_broken_link_rejected(self):
        files = dict(GOOD)
        files["plan.md"] = "# plan\n\nSee [nowhere](gone.md).\n"
        with tempfile.TemporaryDirectory() as tmp:
            path = tree(Path(tmp), files) / "plan.md"
            with self.assertRaisesRegex(ValueError, "broken link gone.md"):
                check_links(path)

    def test_broken_anchor_rejected(self):
        files = dict(GOOD)
        files["plan.md"] = "# plan\n\nSee [the wrong bit](README.md#absent).\n"
        with tempfile.TemporaryDirectory() as tmp:
            path = tree(Path(tmp), files) / "plan.md"
            with self.assertRaisesRegex(ValueError, "broken anchor"):
                check_links(path)

    def test_trailing_whitespace_rejected(self):
        files = dict(GOOD)
        files["plan.md"] = "# plan\n\nA line with trailing space. \n"
        with tempfile.TemporaryDirectory() as tmp:
            path = tree(Path(tmp), files) / "plan.md"
            with self.assertRaisesRegex(ValueError, "trailing whitespace"):
                check_links(path)

    def test_remote_link_skipped(self):
        files = dict(GOOD)
        files["plan.md"] = ("# plan\n\n"
                            "[The web](https://example.com/a(b)) is fine.\n")
        with tempfile.TemporaryDirectory() as tmp:
            path = tree(Path(tmp), files) / "plan.md"
            check_links(path)  # no exception: the URL is not a local path


class UpstreamTests(unittest.TestCase):
    def test_good_record_passes(self):
        with tempfile.TemporaryDirectory() as tmp:
            base = tree(Path(tmp), GOOD)
            artifacts, notes = check_upstream(base)
            self.assertEqual((artifacts, notes), (3, 1))

    def test_missing_index_rejected(self):
        files = {k: v for k, v in GOOD.items() if k != "upstream/README.md"}
        with tempfile.TemporaryDirectory() as tmp:
            base = tree(Path(tmp), files)
            with self.assertRaisesRegex(ValueError, "index is missing"):
                check_upstream(base)

    def test_unlisted_artifact_rejected(self):
        files = dict(GOOD, **{"upstream/0000-cover.patch": "From: a <b>\n"})
        files["upstream/README.md"] = ("# upstream index\n\n"
                                    "- note.md, a finding\n")
        with tempfile.TemporaryDirectory() as tmp:
            base = tree(Path(tmp), files)
            with self.assertRaisesRegex(ValueError,
                                      "does not list 0000-cover.patch"):
                check_upstream(base)

    def test_orphan_note_rejected(self):
        files = dict(GOOD)
        files["README.md"] = "# index\n\n- no links to upstream here\n"
        with tempfile.TemporaryDirectory() as tmp:
            base = tree(Path(tmp), files)
            with self.assertRaisesRegex(ValueError, "linked from nowhere"):
                check_upstream(base)


class DocsTests(unittest.TestCase):
    def test_good_tree_passes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tree(root, GOOD)
            check_docs(root)  # no exception

    def test_dead_upstream_reference_rejected(self):
        # The debt this check exists to catch: a plan cites a note that was
        # never written (goip-parity-events.md UB0-T02's three dead links).
        files = dict(GOOD)
        files["plan.md"] = "# plan\n\nSee [the note](upstream/owed.md).\n"
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tree(root, files)
            with self.assertRaisesRegex(ValueError,
                                      "broken link upstream/owed.md"):
                check_docs(root)


if __name__ == "__main__":
    unittest.main()
