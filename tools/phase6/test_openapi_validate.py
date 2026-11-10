#!/usr/bin/env python3
"""Semantic negative checks for the public API contract gate; no extra packages."""
import copy
import json
import pathlib
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]

class ContractGate(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # JSON is a YAML subset. Ruby also reads the historical YAML source.
        result = subprocess.run(['ruby', '-ryaml', '-rjson', '-e',
            'puts JSON.generate(YAML.safe_load(File.read(ARGV[0])))',
            str(ROOT / 'contracts/openapi/v1/openapi.yaml')], check=True, capture_output=True, text=True)
        cls.source = json.loads(result.stdout)

    def lint(self, document):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / 'contract.yaml'
            path.write_text(json.dumps(document))
            return subprocess.run(['ruby', str(ROOT/'tools/phase6/openapi_validate.rb'), str(path)], capture_output=True, text=True)

    def test_source_passes(self):
        self.assertEqual(self.lint(self.source).returncode, 0)

    def test_rejects_unsafe_and_incomplete_contracts(self):
        def no_auth(d): pass  # The selected GET below avoids depending on path order.
        def internal_property(d): d['components']['schemas']['NodeSummary']['properties']['boot_id'] = {'type':'string'}
        def dangling_ref(d): d['components']['schemas']['NodeSummary']['properties']['id'] = {'$ref':'#/components/schemas/Missing'}
        def open_object(d): d['components']['schemas']['NodeSummary']['additionalProperties'] = True
        for edit in [no_auth, internal_property, dangling_ref, open_object]:
            with self.subTest(edit=edit.__name__):
                document = copy.deepcopy(self.source)
                if edit is no_auth:
                    path = next(p for p,i in document['paths'].items() if 'get' in i)
                    document['paths'][path]['get']['security'] = []
                else: edit(document)
                self.assertNotEqual(self.lint(document).returncode, 0)

    def test_refresh_exception_cannot_be_reused(self):
        if self.source['info']['version'].split('.')[0] != '3': self.skipTest('candidate v3 gate')
        d = copy.deepcopy(self.source)
        d['paths']['/api/v1/me']['get'].update(security=[], **{'x-vortex-refresh-auth':'rotating-body-token'})
        self.assertNotEqual(self.lint(d).returncode, 0)
        d = copy.deepcopy(self.source)
        d['components']['schemas']['RefreshRequest']['required'] = []
        self.assertNotEqual(self.lint(d).returncode, 0)

    def test_rejects_bare_cas_and_missing_error(self):
        d = copy.deepcopy(self.source)
        op = d['paths']['/api/v1/tenants/{tenant_id}/rules/{rule_id}']['put']
        parameter = next(p for p in op['parameters'] if p['name'] == 'If-Match')
        parameter['schema']['pattern'] = '^[0-9]+$'
        self.assertNotEqual(self.lint(d).returncode, 0)
        d = copy.deepcopy(self.source)
        del d['paths']['/api/v1/me']['get']['responses']['503']
        self.assertNotEqual(self.lint(d).returncode, 0)

    def test_business_revision_exception_is_narrow(self):
        d = copy.deepcopy(self.source)
        d['components']['schemas']['NodeSummary']['properties']['target_revision'] = {'type':'integer'}
        self.assertNotEqual(self.lint(d).returncode, 0)

if __name__ == '__main__': unittest.main()
