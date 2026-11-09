"""Release download fallback cannot turn failed/partial bytes into success."""
import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("release_download", Path(__file__).parents[1] / "download_release.py")
downloader = importlib.util.module_from_spec(spec)
spec.loader.exec_module(downloader)


class ReleaseDownload(unittest.TestCase):
    def exercise(self, codes):
        seen = []
        with tempfile.TemporaryDirectory() as work:
            target = Path(work) / "asset.zip"

            def run(args):
                seen.append(args)
                Path(args[-1]).write_bytes(b"asset" if codes[len(seen)-1] == 0 else b"partial")
                return subprocess.CompletedProcess(args, codes[len(seen)-1])

            with patch.object(downloader.subprocess, "run", run):
                try:
                    downloader.download("https://github.com/owner/repo/releases/download/v1/asset.zip", target)
                except subprocess.CalledProcessError:
                    self.assertFalse(target.exists())
                    raise
            self.assertEqual(target.read_bytes(), b"asset")
        return seen

    def test_success_does_not_retry(self):
        self.assertEqual(len(self.exercise([0])), 1)

    def test_failure_retries_same_official_asset_with_query(self):
        seen = self.exercise([22, 0])
        self.assertEqual(seen[1][-3], seen[0][-3] + "?download=1")
        self.assertEqual(seen[1][:-3], seen[0][:-3])

    def test_all_failures_propagate_and_remove_partial_asset(self):
        with self.assertRaises(subprocess.CalledProcessError):
            self.exercise([22, 56, 22])

    def test_api_fallback_keeps_exact_release_asset(self):
        import json
        seen = []
        url = "https://github.com/owner/repo/releases/download/v1/asset.zip"
        with tempfile.TemporaryDirectory() as work:
            target = Path(work) / "asset.zip"
            def run(args):
                seen.append(args)
                out = Path(args[-1])
                if len(seen) <= 2:
                    out.write_bytes(b"partial")
                    return subprocess.CompletedProcess(args, 22)
                if len(seen) == 3:
                    out.write_text(json.dumps(dict(tag_name="v1", assets=[dict(name="asset.zip", browser_download_url=url, url="https://api.github.com/repos/owner/repo/releases/assets/123") ])))
                else:
                    self.assertIn("Accept: application/octet-stream", args)
                    out.write_bytes(b"asset")
                return subprocess.CompletedProcess(args, 0)
            with patch.object(downloader.subprocess, "run", run):
                downloader.download(url, target)
            self.assertEqual(target.read_bytes(), b"asset")
            self.assertEqual(len(seen), 4)

    def test_api_metadata_cannot_redirect_to_another_origin(self):
        import json
        calls = 0
        with tempfile.TemporaryDirectory() as work:
            target = Path(work) / "asset.zip"
            def run(args):
                nonlocal calls
                calls += 1
                out = Path(args[-1])
                if calls <= 2:
                    out.write_bytes(b"partial")
                    return subprocess.CompletedProcess(args, 22)
                out.write_text(json.dumps(dict(tag_name="v1", assets=[dict(name="asset.zip", browser_download_url="https://github.com/owner/repo/releases/download/v1/asset.zip", url="https://other.invalid/asset") ])))
                return subprocess.CompletedProcess(args, 0)
            with patch.object(downloader.subprocess, "run", run), self.assertRaises(AssertionError):
                downloader.download("https://github.com/owner/repo/releases/download/v1/asset.zip", target)
            self.assertEqual(calls, 3)
            self.assertFalse(target.exists())
