"""The reviewed dependency union must be independent of the producer host."""
import hashlib
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import dependency_lock_check as checker


class InventoryHostTests(unittest.TestCase):
    def test_linux_and_darwin_include_the_same_reviewed_targets(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "backend").mkdir()
            (root / "backend/go.sum").write_text("example.org/dep v1.0.0 h1:reviewed\n")
            module = root / "dep"
            module.mkdir()
            (module / "LICENSE").write_text("reviewed license\n")
            inventories = []
            for host in [("linux", "amd64"), ("linux", "arm64"), ("darwin", "arm64")]:
                targets = set()

                def command(args, **kwargs):
                    if args[:2] == ["go", "env"]:
                        return host[0] if args[2] == "GOOS" else host[1]
                    env = kwargs["env"]
                    targets.add((env["GOOS"], env["GOARCH"]))
                    return f"example.org/dep|v1.0.0|{module}"

                with patch.object(checker, "ROOT", root), patch.object(checker, "command", command):
                    inventories.append(checker.inventory())
                self.assertEqual(targets, {("linux", "amd64"), ("linux", "arm64"), ("darwin", "arm64")})
            self.assertEqual(inventories[0], inventories[1])
            self.assertEqual(inventories[0], inventories[2])
            self.assertEqual(inventories[0], [dict(module="example.org/dep", version="v1.0.0",
                sum="h1:reviewed", license_files={"LICENSE": hashlib.sha256(b"reviewed license\n").hexdigest()})])


if __name__ == "__main__":
    unittest.main()
