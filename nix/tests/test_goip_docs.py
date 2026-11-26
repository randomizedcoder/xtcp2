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
    # The blob set (goip_docs.py's check_monitor_blobs): a fixture the docs
    # check needs, one named blob — GOOD's plan names it.
    blob = tmp / "internal" / "goip" / "testdata" / "monitor"
    blob.mkdir(parents=True)
    (blob / "bond-partner-hionly.spec").write_text(
        '{"type":16,"flags":0,"seq":0,"pid":0,"body":"",'
        '"why":"the named"}\n')
    return base


GOOD = {
    "README.md": (
        "# index\n"
        "\n"
        "- [plan](plan.md) names [the bond bug](upstream/note.md).\n"
    ),
    "plan.md": "# plan\n\nSee [the index](README.md#index) and the rows,\n"
               "bond-partner-hionly.spec the named blob.\n",
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
    def test_orphan_blob_rejected(self):
        # The blob the record has lost: committed, but named from no
        # document, so it fails the check rather than rot (EP2-T03's gate:
        # the blob files count as artifacts, no orphans).
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tree(root, GOOD)
            orphan = root / "internal" / "goip" / "testdata" / "monitor" / "linkpd-0009.nl"
            orphan.write_text(
                '{"type":16,"flags":0,"seq":0,"pid":0,"body":"",'
                '"why":"the orphan"}\n')
            with self.assertRaisesRegex(ValueError, "linkpd-0009.nl: named"):
                check_docs(root)

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
