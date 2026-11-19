import copy
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('sync',Path(__file__).with_name('sync-model-aliases.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)


def model(name, provider='antigravity'):
    return {'id':name,'owned_by':provider}


class FakeAPI:
    def __init__(self):
        self.entries={'codex':[{'name':'real','alias':'manual'}]}
        self.native=[model('gemini-4-neon-xhigh')]
        self.applied=[]
        self.visible=True
        self.lose_response=False
    def aliases(self):return copy.deepcopy(self.entries)
    def models(self):return self.native+[model(x['alias']) for x in self.entries.get('antigravity',[]) if self.visible]
    def config(self):return {'request-retry':0,'keys':'private-unchanged'}
    def patch(self,entries):
        self.applied.append(copy.deepcopy(entries))
        if entries:self.entries['antigravity']=copy.deepcopy(entries)
        else:self.entries.pop('antigravity',None)
        if self.lose_response:
            self.lose_response=False
            raise RuntimeError('request lost')


class Tests(unittest.TestCase):
    def test_batch_current_models(self):
        data=[model(x) for x in ['gemini-4-neon-xhigh','gemini-4-argon-high','gemini-4-hydrogen-xhigh','gemini-3.8-flash-high','gemini-3.8-flash-tiered','gemini-3.1-flash-lite']]
        self.assertEqual({x['alias'] for x in m.plan(data,[])['added']},
                         {'gemini-4-neon','gemini-4-argon','gemini-4-hydrogen','gemini-3.8-flash'})
    def test_current_claude_models_have_same_version_hyphen_and_dot_names(self):
        entries=m.plan([model('claude-sonnet-5-5-high'),model('claude-opus-5-5-high')],[])['added']
        self.assertEqual({x['alias'] for x in entries},
                         {'claude-sonnet-5-5','claude-sonnet-5.5','claude-opus-5-5','claude-opus-5.5'})
        self.assertTrue(all(x['fork'] for x in entries))
    def test_claude_dot_native_version_can_supply_hyphen_alias(self):
        entries=m.plan([model('claude-sonnet-5.5-high')],[])['added']
        self.assertEqual({x['alias'] for x in entries},{'claude-sonnet-5.5','claude-sonnet-5-5'})
    def test_claude_ambiguity_is_preserved_for_both_spellings(self):
        result=m.plan([model('claude-opus-5-5-high'),model('claude-opus-5-5-xhigh')],[])
        self.assertFalse(result['added'])
        self.assertEqual(set(result['ambiguous']),{'claude-opus-5-5','claude-opus-5.5'})
    def test_manual_claude_alias_is_not_retargeted(self):
        manual={'name':'manual-backend','alias':'claude-sonnet-5.5','force-mapping':True}
        result=m.plan([model('claude-sonnet-5-5-high')],[manual])
        self.assertEqual(result['merged'][0],manual)
        self.assertEqual([x['alias'] for x in result['added']],['claude-sonnet-5-5'])
    def test_claude_real_base_and_other_providers_keep_their_names(self):
        result=m.plan([model('claude-sonnet-5-5-high'),model('claude-sonnet-5.5'),model('claude-opus-5-5-high','claude')],[])
        self.assertEqual([x['alias'] for x in result['added']],['claude-sonnet-5-5'])
    def test_manual_mapping_and_options_preserved(self):
        entry={'name':'manual-target','alias':'gemini-4-neon','fork':False,'display-name':'Mine'}
        result=m.plan([model('gemini-4-neon-xhigh')],[entry])
        self.assertFalse(result['added']);self.assertEqual(result['merged'],[entry])
    def test_ambiguous_variants_are_not_guessed(self):
        result=m.plan([model('gemini-4-neon-high'),model('gemini-4-neon-xhigh')],[])
        self.assertFalse(result['added']);self.assertIn('gemini-4-neon',result['ambiguous'])
    def test_native_base_takes_priority(self):
        self.assertFalse(m.plan([model('gemini-4-neon'),model('gemini-4-neon-xhigh')],[])['added'])
    def test_other_providers_and_media_models_are_untouched(self):
        self.assertFalse(m.plan([model('gemini-4-neon-xhigh','google'),model('claude-opus-high'),model('gemini-image-preview'),model('gemini-3.8-flash-tiered')],[])['added'])
    def test_case_insensitive_existing_alias(self):
        entry={'name':'custom','alias':'Gemini-4-Neon'}
        self.assertFalse(m.plan([model('gemini-4-neon-xhigh')],[entry])['added'])
    def test_alias_that_looks_like_native_variant_is_not_reused(self):
        entry={'name':'different','alias':'gemini-4-neon-high'}
        self.assertFalse(m.plan([model('gemini-4-neon-high')],[entry])['added'])
    def test_preview_does_not_write(self):
        api=FakeAPI()
        with tempfile.TemporaryDirectory() as root:
            result=m.synchronize(api,Path(root))
            self.assertEqual(result['phase'],'preview');self.assertFalse(list(Path(root).iterdir()))
        self.assertFalse(api.applied)
    def test_batch_is_idempotent_and_preserves_other_providers(self):
        api=FakeAPI();before=copy.deepcopy(api.entries['codex'])
        with tempfile.TemporaryDirectory() as root:
            self.assertEqual(m.synchronize(api,Path(root),True)['phase'],'applied')
            self.assertEqual(m.synchronize(api,Path(root),True)['phase'],'unchanged')
        self.assertEqual(len(api.applied),1);self.assertEqual(api.entries['codex'],before)
    def test_lost_patch_response_is_reconciled(self):
        api=FakeAPI();api.lose_response=True
        with tempfile.TemporaryDirectory() as root:self.assertEqual(m.synchronize(api,Path(root),True)['phase'],'applied')
        self.assertEqual(len(api.applied),1)
    def test_missing_routes_roll_back_only_our_provider(self):
        api=FakeAPI();api.visible=False;before=copy.deepcopy(api.entries)
        with tempfile.TemporaryDirectory() as root:
            with self.assertRaisesRegex(RuntimeError,'catalog'):m.synchronize(api,Path(root),True,sleep=lambda _:None)
        self.assertEqual(api.entries,before);self.assertEqual(len(api.applied),2)
    def test_concurrent_manual_edit_is_preserved(self):
        api=FakeAPI();original=api.aliases;count=0
        def changing():
            nonlocal count
            count+=1
            if count==2:api.entries['antigravity']=[{'name':'handpicked','alias':'mine'}]
            return original()
        api.aliases=changing
        with tempfile.TemporaryDirectory() as root:
            with self.assertRaisesRegex(RuntimeError,'concurrently'):m.synchronize(api,Path(root),True)
        self.assertFalse(api.applied);self.assertEqual(api.entries['antigravity'][0]['name'],'handpicked')
    def test_later_manual_edit_is_not_overwritten_by_rollback(self):
        api=FakeAPI();original=api.patch
        def changing(entries):
            original(entries)
            api.entries['antigravity']=[{'name':'user-target','alias':'user-choice'}]
        api.patch=changing
        with tempfile.TemporaryDirectory() as root:
            with self.assertRaisesRegex(RuntimeError,'confirmed'):m.synchronize(api,Path(root),True)
        self.assertEqual(len(api.applied),1);self.assertEqual(api.entries['antigravity'][0]['name'],'user-target')


@unittest.skipUnless(importlib.util.find_spec('fcntl'),'Native file-lock tests run on Termux')
class ScheduleTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.root=Path(self.temp.name)
        self.api=FakeAPI()
    def tearDown(self):self.temp.cleanup()
    def invoke(self):
        stream=io.StringIO()
        with patch.object(m,'DATA',self.root),patch.object(m,'API',return_value=self.api) as client,patch('sys.argv',['sync','--if-due']),contextlib.redirect_stdout(stream):
            m.main()
        return client,stream.getvalue()
    def test_disabled_policy_does_not_contact_cpa(self):
        client,_=self.invoke();client.assert_not_called()
    def test_not_due_does_not_contact_cpa(self):
        m.atomic(self.root/'model-alias-sync-policy.json',{'enabled':True,'intervalSeconds':900})
        m.atomic(self.root/'model-alias-sync-status.json',{'nextRunAt':int(time.time())+3600})
        client,_=self.invoke();client.assert_not_called()
    def test_due_policy_applies_and_schedules_next_check(self):
        m.atomic(self.root/'model-alias-sync-policy.json',{'enabled':True,'intervalSeconds':900})
        self.invoke();status=m.read_json(self.root/'model-alias-sync-status.json',{})
        self.assertEqual(status['phase'],'applied');self.assertGreater(status['nextRunAt'],time.time()+890)
    def test_live_lock_blocks_duplicate_worker(self):
        import fcntl
        with (self.root/'model-alias-sync.lock').open('a') as lock:
            fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
            client,out=self.invoke()
            client.assert_not_called();self.assertEqual(json.loads(out)['phase'],'busy')


if __name__=='__main__':unittest.main()
