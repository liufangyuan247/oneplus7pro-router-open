"""Sequential free-first search and a private, quota-aware Tavily key pool.

One instance per Waitress process. No background polling or extra service.
"""
from collections import OrderedDict
from copy import deepcopy
from datetime import datetime, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import threading
import time
from urllib.parse import urlsplit


class SearchUnavailable(RuntimeError):
    """Contains only a safe, static message suitable for public errors/logs."""


def atomic_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temporary = path.with_suffix('.tmp')
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, 'w', encoding='utf-8') as handle:
        json.dump(value, handle)
    temporary.chmod(0o600)
    temporary.replace(path)


class SearchRouter:
    def __init__(self, data_dir, transport, primary, clock=time.monotonic, month=None):
        self.data = Path(data_dir)
        self.transport = transport
        self.primary = primary
        self.clock = clock
        self.month = month or (lambda: datetime.now(timezone.utc).strftime('%Y-%m'))
        self.lock = threading.Lock()
        self.cache = OrderedDict()
        self.cooldown = {}
        self.primary_until = 0

    def search(self, query, mode='auto'):
        query = query.strip()
        if not query or len(query) >= 500 or mode not in ('auto', 'tavily'):
            raise SearchUnavailable('Invalid search query or mode')
        # Bound waiting; a timed-out SearXNG request must not later spend credits.
        if not self.lock.acquire(timeout=0.5):
            raise SearchUnavailable('Search busy; retry later')
        try:
            cache_key = (mode, query)
            cached = self.cache.get(cache_key)
            if cached and cached[0] > self.clock():
                self.cache.move_to_end(cache_key)
                return deepcopy(cached[1])
            deadline = self.clock() + 20
            rows = None
            if mode == 'auto' and self.clock() >= self.primary_until:
                try:
                    rows = self.primary(query)
                except Exception:
                    # Never expose provider exception bodies, headers or keys.
                    self.primary_until = self.clock() + 300
            if not rows:
                rows = self._tavily(query, deadline)
            self.cache[cache_key] = (self.clock() + 300, deepcopy(rows))
            self.cache.move_to_end(cache_key)
            while len(self.cache) > 64:
                self.cache.popitem(last=False)
            return rows
        finally:
            self.lock.release()

    def _request(self, method, endpoint, key, deadline, **kwargs):
        remaining = deadline - self.clock()
        if remaining < 0.5:
            raise SearchUnavailable('Search deadline exceeded')
        try:
            return self.transport(method, 'https://api.tavily.com/' + endpoint,
                                  headers={'Authorization': 'Bearer ' + key},
                                  timeout=min(5, remaining), **kwargs)
        except Exception:
            raise SearchUnavailable('Search provider network unavailable') from None

    @staticmethod
    def _within_free_quota(usage):
        account = usage.get('account', {})
        key = usage.get('key', {})
        used, limit = account.get('plan_usage'), account.get('plan_limit')
        if (type(used) not in (int, float) or type(limit) not in (int, float)
                or not math.isfinite(used) or not math.isfinite(limit)
                or used < 0 or used + 1 > min(limit, 1000)):
            return False
        if key.get('limit') is not None:
            used, limit = key.get('usage'), key['limit']
            if (type(used) not in (int, float) or type(limit) not in (int, float)
                    or not math.isfinite(used) or not math.isfinite(limit)
                    or used < 0 or used + 1 > limit):
                return False
        return True

    def _tavily(self, query, deadline):
        try:
            configured = json.loads((self.data / 'tavily-keys.json').read_text())['keys']
            keys = list(dict.fromkeys(x['api_key'].strip() for x in configured))
            if not keys or len(keys) > 8 or any(not k.startswith('tvly-') for k in keys):
                raise ValueError()
            state_path = self.data / 'tavily-state.json'
            state = json.loads(state_path.read_text()) if state_path.exists() else {}
            if state.get('month') != self.month():
                state = {'month': self.month(), 'next': 0, 'attempts': {}}
            start = state.get('next', 0) % len(keys)
        except Exception:
            raise SearchUnavailable('Tavily private configuration unavailable') from None

        for offset in range(len(keys)):
            index = (start + offset) % len(keys)
            key = keys[index]
            identity = hashlib.sha256(key.encode()).hexdigest()[:20]
            if self.cooldown.get(identity, 0) > self.clock():
                continue
            if state['attempts'].get(identity, 0) >= 1000:
                continue
            try:
                usage = self._request('GET', 'usage', key, deadline)
                if usage.status_code != 200 or not self._within_free_quota(usage.json()):
                    self.cooldown[identity] = self.clock() + 60
                    continue
                # Reserve before sending: an ambiguous timeout still counts.
                state['attempts'][identity] = state['attempts'].get(identity, 0) + 1
                state['next'] = (index + 1) % len(keys)
                atomic_json(state_path, state)
                response = self._request('POST', 'search', key, deadline, json={
                    'query': query, 'search_depth': 'basic', 'auto_parameters': False,
                    'max_results': 5, 'include_answer': False, 'include_raw_content': False,
                })
                if response.status_code != 200:
                    self.cooldown[identity] = self.clock() + 60
                    continue
                rows = self._parse(response.json())
                return rows
            except Exception:
                self.cooldown[identity] = self.clock() + 60
        raise SearchUnavailable('Tavily unavailable or free quota exhausted')

    @staticmethod
    def _parse(payload):
        rows, seen = [], set()
        for item in payload['results']:
            url, title = item.get('url', ''), item.get('title', '')
            try:
                parsed = urlsplit(url)
                safe = parsed.scheme in ('http', 'https') and parsed.hostname
            except (TypeError, ValueError):
                safe = False
            if safe and title and url not in seen:
                rows.append({'title': title, 'url': url, 'content': item.get('content') or ''})
                seen.add(url)
            if len(rows) == 5:
                break
        return rows


_instance = None
_instance_lock = threading.Lock()


def get_router():
    global _instance
    with _instance_lock:
        if _instance is None:
            from curl_cffi import requests
            from searx.engines import engines
            session = requests.Session(trust_env=False)
            proxy = os.environ.get('SEARXNG_SEARCH_PROXY', 'http://127.0.0.1:17890')

            def transport(method, url, **kwargs):
                return session.request(method, url, proxy=proxy, allow_redirects=False, **kwargs)

            def primary(query):
                engine = engines['duckduckgo html get']
                params = {}
                engine.request(query, params)
                response = transport('GET', params['url'], headers=params['headers'], timeout=4)
                if response.status_code != 200:
                    raise SearchUnavailable('DuckDuckGo unavailable')
                return engine.response(response)

            _instance = SearchRouter(Path.home() / 'router-data/searxng', transport, primary)
    return _instance
