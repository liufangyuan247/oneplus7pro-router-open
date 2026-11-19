#!/data/data/com.termux/files/usr/bin/python
"""Batch Gemini/Claude aliases from Antigravity using CPA's management API."""
import argparse
import copy
import datetime as dt
import json
import os
from pathlib import Path
import re
import time
import urllib.error
import urllib.request
import uuid

DATA = Path.home() / 'router-data/cpa'
SUFFIX = re.compile(r'^((?:gemini|claude)-[a-z0-9][a-z0-9.-]*)-(?:xhigh|high|medium|low)$')
CLAUDE_VERSION = re.compile(r'^(claude-(?:opus|sonnet|haiku)-)(\d+)([.-])(\d+)$')
INTERVAL = 900


def read_json(path, default):
    try:
        return json.loads(path.read_text(encoding='utf-8'))
    except FileNotFoundError:
        return default


def atomic(path, data):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temp = path.with_name(path.name + '.new-' + uuid.uuid4().hex)
    with temp.open('w', encoding='utf-8') as stream:
        os.chmod(temp, 0o600)
        json.dump(data, stream, ensure_ascii=False, indent=2)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temp, path)


def plan(models, aliases):
    """Add only unambiguous same-version aliases; never edit existing entries."""
    existing = {x['alias'].casefold() for x in aliases}
    native = {x['id'] for x in models if x.get('owned_by') == 'antigravity'
              and isinstance(x.get('id'), str) and x['id'].casefold() not in existing}
    native_lookup = {x.casefold() for x in native}
    candidates = {}
    for name in sorted(native):
        match = SUFFIX.fullmatch(name)
        if match:
            base = match[1]
            # Do not turn an unversioned provider name into a latest-model alias.
            if base.startswith('claude-') and not re.search(r'(?:^|-)\d', base[7:]):
                continue
            names = {base}
            version = CLAUDE_VERSION.fullmatch(base)
            if version:
                alternate = '.' if version[3] == '-' else '-'
                names.add(version[1] + version[2] + alternate + version[4])
            for alias in sorted(names):
                candidates.setdefault(alias, []).append(name)
    added, ambiguous, preserved = [], {}, []
    for alias, targets in sorted(candidates.items()):
        if alias.casefold() in existing:
            preserved.append(alias)
        elif alias.casefold() in native_lookup:
            continue  # A real upstream model always keeps its own name.
        elif len(targets) != 1:
            ambiguous[alias] = targets
        else:
            added.append({'name': targets[0], 'alias': alias, 'fork': True})
    return {'added': added, 'ambiguous': ambiguous, 'preserved': preserved,
            'merged': copy.deepcopy(aliases) + added}


class API:
    def __init__(self, data):
        self.management = (data / 'management-key').read_text().strip()
        self.client = (data / 'api-key').read_text().strip()
        self.http = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def request(self, path, method='GET', payload=None, client=False):
        req = urllib.request.Request('http://127.0.0.1:8317' + path,
            data=None if payload is None else json.dumps(payload).encode(), method=method,
            headers={'Authorization': 'Bearer ' + (self.client if client else self.management),
                     'Content-Type': 'application/json'})
        try:
            with self.http.open(req, timeout=8) as response:
                raw = response.read(2 * 1024**2 + 1)
        except urllib.error.HTTPError as error:
            raise RuntimeError('CPA management/catalog HTTP ' + str(error.code)) from None
        except (urllib.error.URLError, TimeoutError, OSError):
            raise RuntimeError('CPA management/catalog unavailable') from None
        if len(raw) > 2 * 1024**2:
            raise RuntimeError('CPA response exceeded size limit')
        return json.loads(raw) if raw else {}

    def aliases(self):
        return self.request('/v0/management/oauth-model-alias').get('oauth-model-alias') or {}

    def models(self):
        return self.request('/v1/models', client=True)['data']

    def config(self):
        value = self.request('/v0/management/config')
        value.pop('oauth-model-alias', None)
        return value

    def patch(self, aliases):
        return self.request('/v0/management/oauth-model-alias', 'PATCH',
                            {'provider': 'antigravity', 'aliases': aliases})


def normalized(entries):
    return sorted([{k: v for k, v in x.items() if v not in ('', None, False)} for x in entries],
                  key=lambda x: x['alias'].casefold())


def synchronize(api, data, apply=False, sleep=time.sleep):
    before = api.aliases()
    aliases = before.get('antigravity') or []
    result = plan(api.models(), aliases)
    summary = {k: v for k, v in result.items() if k != 'merged'}
    summary['phase'] = 'preview' if not apply else 'unchanged'
    if not apply or not result['added']:
        return summary
    config = api.config()
    if api.aliases() != before:
        raise RuntimeError('Aliases changed concurrently; no changes applied')
    backup = data / 'model-alias-backups' / (dt.datetime.now().strftime('%Y%m%d-%H%M%S') + '-' + uuid.uuid4().hex + '.json')
    atomic(backup, {'provider': 'antigravity', 'previous': aliases, 'added': result['added']})
    expected = normalized(result['merged'])
    try:
        # A lost PATCH response is reconciled by the following read.
        try:
            api.patch(result['merged'])
        except RuntimeError:
            if normalized(api.aliases().get('antigravity') or []) != expected:
                raise
        current = api.aliases()
        if normalized(current.get('antigravity') or []) != expected:
            raise RuntimeError('Alias update could not be confirmed')
        if {k:v for k,v in current.items() if k != 'antigravity'} != {k:v for k,v in before.items() if k != 'antigravity'}:
            raise RuntimeError('Other providers changed concurrently')
        if api.config() != config:
            raise RuntimeError('Other configuration changed during update')
        required = {x['alias'] for x in result['added']}
        for attempt in range(6):
            visible = {x['id'] for x in api.models() if x.get('owned_by') == 'antigravity'}
            if required <= visible:
                summary.update(phase='applied', backup=backup.name)
                return summary
            if attempt < 5:
                sleep(1)
        raise RuntimeError('Aliases did not appear in the model catalog')
    except Exception:
        # Restore our provider only if nobody has subsequently edited it.
        current = api.aliases().get('antigravity') or []
        if normalized(current) == expected:
            api.patch(aliases)
            if normalized(api.aliases().get('antigravity') or []) != normalized(aliases):
                raise RuntimeError('Alias rollback requires manual review') from None
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group()
    group.add_argument('--apply', action='store_true', help='apply one batch now')
    group.add_argument('--if-due', action='store_true', help='scheduled, opt-in maintenance')
    group.add_argument('--status', action='store_true')
    args = parser.parse_args()
    status_file = DATA / 'model-alias-sync-status.json'
    policy = read_json(DATA / 'model-alias-sync-policy.json', {'enabled': False, 'intervalSeconds': INTERVAL})
    if args.status:
        print(json.dumps({'policy': policy, **read_json(status_file, {'phase':'never-run'})}, ensure_ascii=False))
        return
    import fcntl  # Android only; planning/tests also run on Windows.
    with (DATA / 'model-alias-sync.lock').open('a') as lock:
        os.chmod(lock.name, 0o600)
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            print('{"phase":"busy"}')
            return
        now = int(time.time())
        if args.if_due and (not policy.get('enabled') or now < read_json(status_file, {}).get('nextRunAt', 0)):
            return
        try:
            result = synchronize(API(DATA), DATA, args.apply or args.if_due)
            result['checkedAt'] = dt.datetime.now(dt.timezone.utc).isoformat()
            if args.apply or args.if_due:
                interval = policy.get('intervalSeconds', INTERVAL)
                if type(interval) is not int or not 300 <= interval <= 86400:
                    interval = INTERVAL
                result['nextRunAt'] = now + interval
                atomic(status_file, result)
            print(json.dumps(result, ensure_ascii=False))
        except Exception as error:
            # Never include HTTP bodies, credentials or arbitrary tracebacks.
            result = {'phase':'failed', 'error':str(error)[:180] if isinstance(error, RuntimeError) else type(error).__name__,
                      'checkedAt':dt.datetime.now(dt.timezone.utc).isoformat(), 'nextRunAt':now + 300}
            if args.apply or args.if_due:
                atomic(status_file, result)
            print(json.dumps(result, ensure_ascii=False))
            raise SystemExit(1)


if __name__ == '__main__':
    os.umask(0o077)
    main()
