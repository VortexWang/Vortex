"""Bounded review evaluator; not a replacement for runtime semantic validation."""
import importlib.util
import json
from pathlib import Path
import sys

sys.dont_write_bytecode = True

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('review_schema', ROOT / 'tests/phase1/test_lifecycle_candidates.py')
review = importlib.util.module_from_spec(spec)
spec.loader.exec_module(review)
schema = json.loads((ROOT / 'contracts/runtime/config.schema.json').read_text())
if __name__ == '__main__':
    review.validate(json.load(sys.stdin), schema)
    print('Runtime schema review projection passed; semantic runtime checks remain required')
