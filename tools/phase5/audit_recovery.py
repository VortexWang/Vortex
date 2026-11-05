#!/usr/bin/env python3
"""Independent fail-closed inspection of 05-04 raw SQLite and fault logs."""
import argparse
import gzip
import hashlib
import json
from pathlib import Path
import sqlite3
import tempfile


def snapshot(out, label, role):
    hashes = json.loads((out / f'{label}-ledgers.json').read_text())
    raw = gzip.decompress((out / f'{label}-{role}.sqlite.gz').read_bytes())
    assert hashlib.sha256(raw).hexdigest() == hashes[role], (label, role, 'hash')
    return raw


def inspect(raw, callback):
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / 'ledger.db'
        path.write_bytes(raw)
        db = sqlite3.connect(f'file:{path}?mode=ro', uri=True)
        assert db.execute('pragma integrity_check').fetchone()[0] == 'ok'
        assert db.execute('pragma journal_mode').fetchone()[0] == 'wal'
        callback(db)
        db.close()


def audit(out):
    meta = json.loads((out / 'recovery-hub.json').read_text())
    raw = gzip.decompress((out / 'recovery-hub.sqlite.gz').read_bytes())
    assert hashlib.sha256(raw).hexdigest() == meta['SHA256']
    assert meta['Info']['SQLiteVersion'] == '3.54.1'
    assert meta['Info']['JournalMode'] == 'wal' and meta['Info']['Synchronous'] == '2'

    def handoff(db):
        assert db.execute('select count(*) from hub_transfers').fetchone()[0] == 1
        assert db.execute('select count(*) from hub_ledger where confirmed=0').fetchone()[0] == 0
        assert db.execute('select signer_epoch from issuer_epochs').fetchone()[0] == '2'
        assert db.execute('select remaining_bytes from issuer_delegations').fetchone()[0] == '512'
        assert db.execute('select count(*) from issuer_issues').fetchone()[0] == 2
        assert db.execute("select value from agent_state where key='ledger_checkpoint'").fetchone()[0] == '3'
        for payload, digest in db.execute('select payload,payload_sha256 from hub_ledger'):
            assert hashlib.sha256(payload).digest() == digest
    inspect(raw, handoff)

    def restarted(db):
        assert db.execute("select count(*) from activation_observations where execution_state='os-process-stopped'").fetchone()[0] >= 1
        assert db.execute("select count(*) from config_results where state='released'").fetchone()[0] == 1
        assert db.execute("select count(*) from config_results where state='active'").fetchone()[0] == 1
        assert db.execute("select count(distinct boot_id) from authorization_records").fetchone()[0] == 2
        assert db.execute("select count(*) from authorization_records where state='settlement_only'").fetchone()[0] == 3
        assert db.execute("select unknown_upper_bound,liability_state from unknown_usage_batches where batch_id='interrupted-write'").fetchone() == ('512', 'unresolved')
        assert db.execute("select count(*) from durable_events where kind='boot-recovery'").fetchone()[0] == 1
        # Every source remains conserved, including reservations interrupted by SIGKILL.
        for node, grant, initial, remaining in db.execute('select node_id,authorization_id,initial_bytes,remaining_bytes from authorization_balances'):
            total = int(remaining)
            for held, state, used in db.execute('select h.reserved_bytes,h.state,u.used_bytes from authorization_usage_holds h join usage_reservations u on u.node_id=h.node_id and u.batch_id=h.batch_id where h.node_id=? and h.authorization_id=?', (node, grant)):
                assert state in ('reserved','unknown','committed','returned')
                total += int(used) if state == 'committed' else int(held) if state in ('reserved','unknown') else 0
            assert total == int(initial), (grant, total, initial)
    inspect(snapshot(out, 'restart', 'agent'), restarted)

    def epoch(db):
        assert db.execute('select remaining_bytes from authorization_handoffs').fetchone()[0] == '4096'
        assert db.execute('select from_epoch,to_epoch from authorization_handoffs').fetchone() == ('1','2')
        assert db.execute("select unknown_upper_bound,liability_state from unknown_usage_batches where batch_id='held-old'").fetchone() == ('512','unresolved')
        assert db.execute("select count(distinct path_generation) from authorization_records").fetchone()[0] == 1
        assert db.execute("select count(distinct signer_epoch) from authorization_records").fetchone()[0] == 2
    inspect(snapshot(out, 'epoch', 'agent'), epoch)
    inspect(snapshot(out, 'epoch', 'hub'), lambda db: None)
    inspect(snapshot(out, 'restart', 'hub'), lambda db: None)
    log = (out / 'recovery-race.log').read_text()
    names = ('TestHandoffFullDiskDoesNotPartiallyTransfer', 'TestHandoffSIGKILLRecovery',
             'TestRecoveryReplaysDurablePrefixAndTailAfterLostACK',
             'TestRecoveryQueriesExecutionBeforeReleasingResources',
             'TestNewExecutorCannotForgeOldAbortProof',
             'TestRuntimeSIGKILLFreshBootReprepareAndRecoverDebt',
             'TestAuthenticatedHubEpochHandoffKeepsOldGrantAndBoundsSuccessor',
             'TestActivationTransitionsRejectLateAndConcurrentResults',
             'TestEpochHandoffRejectsMutationAndRevocationBeforeTransfer')
    for name in names:
        assert f'--- PASS: {name} ' in log, name
        assert f'--- SKIP: {name}' not in log, name
    return {'schema': 1, 'status': 'passed', 'snapshots': 5, 'fault_cases': len(names), 'backend': 'protocol-test-only'}


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('directory', type=Path)
    args = parser.parse_args()
    print(json.dumps(audit(args.directory)))
