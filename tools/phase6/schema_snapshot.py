"""Deterministic review copy of the executable, forward migration schema."""
from pathlib import Path
root = Path(__file__).resolve().parents[2]
migrations = sorted((root / 'backend/db/migrations').glob('*.sql'))
parts = [f'-- Phase 6 implemented PostgreSQL schema through migration {migrations[-1].stem}.\n-- Canonical executable source: backend/db/migrations/*.sql.\n']
for migration in migrations:
    up = migration.read_text().split('-- +goose Down', 1)[0]
    parts.append('\n'.join(line for line in up.splitlines() if not line.startswith('-- +goose')) + '\n')
(root / 'contracts/database/schema.sql').write_text('\n'.join(parts))
