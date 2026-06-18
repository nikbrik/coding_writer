import tempfile
import unittest
from pathlib import Path
from unittest import mock

from .compress import compress_file


class CompressSafeguardsTest(unittest.TestCase):
    def test_secret_content_blocks_before_model_call(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "notes.md"
            path.write_text("# Notes\nOPENROUTER_API_KEY=sk-secret123456789\n")
            with mock.patch("scripts.compress.call_claude") as call:
                with self.assertRaisesRegex(ValueError, "content looks sensitive"):
                    compress_file(path, yes=True)
                call.assert_not_called()
            self.assertIn("OPENROUTER_API_KEY", path.read_text())

    def test_dry_run_makes_no_model_call_or_write(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "notes.md"
            original = "# Notes\nLong natural language paragraph for compression.\n"
            path.write_text(original)
            with mock.patch("scripts.compress.call_claude") as call:
                self.assertTrue(compress_file(path, dry_run=True))
                call.assert_not_called()
            self.assertEqual(path.read_text(), original)


if __name__ == "__main__":
    unittest.main()
