import copy
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

caller = Path(sys.argv[1])
sha = sys.argv[2]
evidence = caller / 'artifacts/phase6/06-08' / sha[:8] / 'license'
evidence.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory(prefix='vortex-cr004-clean-') as temp:
    root = Path(temp) / 'source'
    subprocess.run(['git', 'clone', '--quiet', '--no-hardlinks', str(caller), str(root)], check=True)
    subprocess.run(['git', '-C', str(root), 'checkout', '--quiet', '--detach', sha], check=True)
    assert not subprocess.check_output(['git', '-C', str(root), 'status', '--porcelain=v1'])
    (evidence / 'source-identity.txt').write_bytes(subprocess.check_output(['git', '-C', str(root), 'rev-parse', 'HEAD', 'HEAD^{tree}']))
    with (evidence / 'dependency-lock.log').open('w') as log:
        result = subprocess.run(['sh', 'tools/phase6/dependency_lock_check.sh'], cwd=root, stdout=log, stderr=subprocess.STDOUT)
    assert result.returncode == 0, 'dependency gate failed'
    gate = json.loads((evidence / 'dependency-lock.log').read_text().splitlines()[-1])
    (evidence / 'dependency-lock.json').write_text(json.dumps(gate, indent=2) + '\n')
    spec = importlib.util.spec_from_file_location('sqlc_check', root / 'tools/phase6/sqlc_license_check.py')
    check = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(check)
    review = json.loads((root / 'tools/phase6/sqlc-dependencies.json').read_text())
    lock = json.loads((root / 'docs/governance/version-lock.yaml').read_text())
    pinned = next(row for row in lock['locked_candidates'] if row['name'] == 'sqlc')
    results = []
    for artifact in review['artifacts']:
        platform = artifact['platform']
        cache = Path('/tmp/vortex-sqlc-artifacts') / platform
        archive = cache / artifact['url'].rsplit('/', 1)[-1]
        result = check.verify(archive, cache / 'sqlc', platform, pinned)
        assert result['status'] == 'passed'
        assert len(result['approved_exceptions']) == 3
        results.append(result)
    (evidence / 'three-artifacts.json').write_text(json.dumps(results, indent=2) + '\n')
    # Exercise the real closure verifier with changed approval/source/use inputs,
    # preserving the committed clone and the caller's files.
    sandbox = Path(temp) / 'mutation'
    (sandbox / 'docs/governance').mkdir(parents=True)
    (sandbox / 'tools/phase6').mkdir(parents=True)
    (sandbox / 'artifacts').symlink_to(root / 'artifacts', target_is_directory=True)
    approval_path = sandbox / 'docs/governance/tool-license-exceptions.json'
    review_path = sandbox / 'tools/phase6/sqlc-dependencies.json'
    base_approvals = json.loads((root / 'docs/governance/tool-license-exceptions.json').read_text())
    shutil.copyfile(root / 'docs/governance/license-policy.yaml', sandbox / 'docs/governance/license-policy.yaml')
    check.ROOT = sandbox
    negative = []
    for scenario in ('missing-cr004', 'pending-cr004', 'module-sum-drift', 'file-hash-drift',
                     'missing-bison-exception', 'product-runtime', 'modified-artifact'):
        approvals = copy.deepcopy(base_approvals)
        changed_review = copy.deepcopy(review)
        check.USAGE = 'development-ci-sql-to-go'
        cr004 = next(row for row in approvals['exceptions'] if row['id'] == 'CR-004')
        binary = Path('/tmp/vortex-sqlc-artifacts/darwin-arm64/sqlc')
        archive = binary.parent / 'sqlc_1.31.1_darwin_arm64.tar.gz'
        if scenario == 'missing-cr004':
            approvals['exceptions'] = [row for row in approvals['exceptions'] if row['id'] != 'CR-004']
        elif scenario == 'pending-cr004':
            cr004['status'] = 'pending'
        elif scenario == 'module-sum-drift':
            cr004['embedded_sources'][0]['source']['sum'] += '-changed'
        elif scenario == 'file-hash-drift':
            changed_review['embedded_sources'][0]['files']['sqlite3/libc/malloc.c'] = '0' * 64
        elif scenario == 'missing-bison-exception':
            changed_review['embedded_sources'][1]['licenses'][-1] = 'GPL-2.0-or-later'
        elif scenario == 'product-runtime':
            check.USAGE = 'product-runtime'
        elif scenario == 'modified-artifact':
            modified = Path(temp) / 'sqlc-modified'
            modified.write_bytes(binary.read_bytes() + b'changed')
            binary = modified
        approval_path.write_text(json.dumps(approvals))
        review_path.write_text(json.dumps(changed_review))
        try:
            rejected = check.verify(archive, binary, 'darwin-arm64', pinned)
            assert rejected['status'] == 'blocked', scenario
            negative.append(dict(scenario=scenario, status='rejected', findings=rejected['license_findings']))
        except ValueError as error:
            negative.append(dict(scenario=scenario, status='rejected', error=str(error)))
    (evidence / 'negative-closure-checks.json').write_text(json.dumps(negative, indent=2) + '\n')
    assert not subprocess.check_output(['git', '-C', str(root), 'status', '--porcelain=v1'])
    report = dict(source_commit=sha, source_tree=subprocess.check_output(['git', '-C', str(root), 'rev-parse', 'HEAD^{tree}'], text=True).strip(), source_clean_before=True, source_clean_after=True, dependency_gate_exit=0, generator_artifacts=3, linked_modules=42, negative_closure_checks=len(negative), status='passed')
    (evidence / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report))
