"""Download a pinned GitHub release asset; callers verify its locked digest."""
from pathlib import Path
import json
import re
import subprocess
import sys
import tempfile


def fetch(url, destination, headers=()):
    return subprocess.run([
        "curl", "--fail", "--location", "--retry", "3",
        "--connect-timeout", "15", "--max-time", "120",
        "--silent", "--show-error", *headers, url, "--output", str(destination),
    ])


def download(url, destination):
    destination = Path(destination)
    # A stale gateway response can be cached at the canonical asset URL.
    # Retry only the same official asset with GitHub's download query; neither
    # a mirror nor different artifact bytes are trusted by the checksum caller.
    for candidate in (url, url + "?download=1"):
        result = fetch(candidate, destination)
        if result.returncode == 0:
            return
        destination.unlink(missing_ok=True)
    match = re.fullmatch(r"https://github.com/([\w.-]+)/([\w.-]+)/releases/download/([\w.-]+)/([\w.-]+)", url)
    if match:
        owner, repo, tag, name = match.groups()
        api = f"https://api.github.com/repos/{owner}/{repo}/releases/"
        with tempfile.TemporaryDirectory(prefix="vortex-release-metadata-") as work:
            metadata = Path(work) / "release.json"
            result = fetch(api + "tags/" + tag, metadata)
            if result.returncode == 0:
                release = json.loads(metadata.read_text())
                assert release["tag_name"] == tag
                assets = [a for a in release["assets"] if a["name"] == name]
                assert len(assets) == 1 and assets[0]["browser_download_url"] == url
                asset_url = assets[0]["url"]
                assert re.fullmatch(re.escape(api) + r"assets/\d+", asset_url)
                result = fetch(asset_url, destination, ("--header", "Accept: application/octet-stream"))
                if result.returncode == 0:
                    return
                destination.unlink(missing_ok=True)
    raise subprocess.CalledProcessError(result.returncode, result.args)


if __name__ == "__main__":
    download(sys.argv[1], sys.argv[2])
