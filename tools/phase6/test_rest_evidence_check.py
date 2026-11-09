#!/usr/bin/env python3
import importlib.util
import json
from pathlib import Path
import shutil
import tempfile
import unittest

SPEC=importlib.util.spec_from_file_location('rest_evidence',Path(__file__).with_name('rest_evidence_check.py'))
GATE=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(GATE)

class EvidenceGate(unittest.TestCase):
    def fixture(self,root):
        sha='a'*40
        (root/'source-identity.txt').write_text(sha+'\n'+'b'*40+'\n')
        for name in ['source-status-before.txt','source-status-after.txt','sqlc-diff.log']:(root/name).write_text('')
        (root/'sqlc-output-count.txt').write_text('33\n')
        (root/'completion.txt').write_text('backend normal/race, two sqlc generations, six cross-builds and source cleanliness passed\n')
        events=[]
        for p,names in GATE.REQUIRED.items():
            events.append({'Action':'pass','Package':p,'Elapsed':1})
            events.extend({'Action':'pass','Package':p,'Test':n} for n in names)
        events.extend({'Action':'pass','Package':'vortex/backend/internal/'+p,'Elapsed':1} for p in ['auth','control','repository','policy','timeauthority'])
        for mode in ['normal','race']:(root/f'backend-{mode}.jsonl').write_text(''.join(json.dumps(e)+'\n' for e in events))
        for name,count in [('native-build-metadata.txt',2),('race-build-metadata.txt',2),('cross-build-metadata.txt',6)]:
            raw=('vcs.revision='+sha+'\nvcs.modified=false\n')*count
            if count==6:raw+=('GOOS=linux\nGOARCH=amd64\n'*3)+('GOOS=linux\nGOARCH=arm64\n'*3)
            (root/name).write_text(raw)
        return sha

    def test_complete_and_negative_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            base=Path(directory)/'base';base.mkdir();sha=self.fixture(base)
            self.assertEqual(GATE.check(base,sha)['result'],'passed')
            edits={
                'wrong_sha':('source-identity.txt','c'*40+'\n'+'b'*40+'\n'),
                'dirty':('source-status-after.txt',' M backend/internal/httpapi/router.go\n'),
                'skip':('backend-race.jsonl',json.dumps({'Action':'skip'})+'\n'),
                'missing_critical':('backend-race.jsonl',json.dumps({'Action':'pass','Package':'vortex/backend/internal/service','Elapsed':1})+'\n'),
                'wrong_generated':('sqlc-output-count.txt','32\n'),
                'modified_binary':('cross-build-metadata.txt','vcs.modified=true\n'),
            }
            for name,(file,raw) in edits.items():
                with self.subTest(name=name):
                    target=Path(directory)/name;shutil.copytree(base,target);(target/file).write_text(raw)
                    with self.assertRaises(AssertionError):GATE.check(target,sha)
if __name__=='__main__':unittest.main()
