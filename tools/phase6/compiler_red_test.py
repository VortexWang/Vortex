"""Supported reporter wrapping the actual compiled artifact authority assertion."""
import subprocess
import unittest
from pathlib import Path

class CompilerAuthority(unittest.TestCase):
    def test_compiled_artifact_authority(self):
        result = subprocess.run(["go", "test", "./internal/compiler", "-run", "^TestCompileArtifactImmutableSchema$", "-count=1", "-v"], cwd=Path(__file__).resolve().parents[2] / "backend", text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        self.assertEqual(result.returncode, 0, result.stdout)

if __name__ == "__main__":
    unittest.main(verbosity=2)
