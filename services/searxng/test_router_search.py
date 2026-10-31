"""No external requests: routing, quotas, rotation, persistence and caching."""
import json
from pathlib import Path
from tempfile import TemporaryDirectory
import threading
from types import SimpleNamespace
import unittest

from router_search import SearchRouter, SearchUnavailable


def reply(status=200, payload=None):
    return SimpleNamespace(status_code=status, json=lambda: payload)


ROWS = [{'title': 'Example', 'url': 'https://example.org/', 'content': 'Evidence'}]


class SearchRouterTests(unittest.TestCase):
    def setUp(self):
        self.tmp = TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.data = Path(self.tmp.name)
        self.keys = ['tvly-test-one', 'tvly-test-two']
        (self.data / 'tavily-keys.json').write_text(json.dumps({
            'keys': [{'api_key': key} for key in self.keys]}))
        self.calls = []
        self.used = dict.fromkeys(self.keys, 0)
        self.errors = {}
        self.now = 1000
        self.current_month = '2026-10'
        self.primary_calls = []

    def transport(self, method, url, **kwargs):
        key = kwargs['headers']['Authorization'].split(' ', 1)[1]
        endpoint = url.rsplit('/', 1)[1]
        self.calls.append((key, endpoint, kwargs))
        error = self.errors.get((key, endpoint))
        if isinstance(error, Exception):
            raise error
        if error:
            return reply(error)
        if endpoint == 'usage':
            return reply(payload={'account': {'plan_usage': self.used[key], 'plan_limit': 1000},
                                  'key': {'usage': self.used[key], 'limit': None}})
        self.used[key] += 1
        return reply(payload={'results': ROWS})

    def primary(self, query):
        self.primary_calls.append(query)
        return ROWS.copy()

    def router(self, primary=None):
        return SearchRouter(self.data, self.transport, primary or self.primary,
                            clock=lambda: self.now, month=lambda: self.current_month)

    def posts(self):
        return [call for call in self.calls if call[1] == 'search']

    def test_primary_success_never_reads_key_or_spends_credit(self):
        (self.data / 'tavily-keys.json').unlink()
        self.assertEqual(self.router().search('test'), ROWS)
        self.assertEqual(self.calls, [])

    def test_primary_failure_falls_back_then_cools_down(self):
        def fail(query):
            self.primary_calls.append(query)
            raise RuntimeError('challenge')
        router = self.router(fail)
        router.search('first')
        router.search('second')
        self.assertEqual(self.primary_calls, ['first'])
        self.assertEqual([x[0] for x in self.posts()], self.keys)
        self.now += 301
        router.search('third')
        self.assertEqual(self.primary_calls, ['first', 'third'])

    def test_rotation_survives_restart_and_cache_does_not_spend(self):
        router = self.router()
        rows = router.search('first', 'tavily')
        rows[0]['title'] = 'Caller mutation'
        self.assertEqual(router.search('first', 'tavily'), ROWS)
        self.assertEqual(len(self.posts()), 1)
        self.router().search('second', 'tavily')
        self.assertEqual([x[0] for x in self.posts()], self.keys)

    def test_requests_are_always_basic_and_bounded(self):
        self.router().search('test', 'tavily')
        payload = self.posts()[0][2]['json']
        self.assertEqual(payload['search_depth'], 'basic')
        self.assertIs(payload['auto_parameters'], False)
        self.assertEqual(payload['max_results'], 5)
        self.assertIs(payload['include_raw_content'], False)
        self.assertEqual(self.primary_calls, [])

    def test_exhausted_key_is_skipped_and_no_paid_requests(self):
        self.used[self.keys[0]] = 1000
        self.router().search('first', 'tavily')
        self.assertEqual([x[0] for x in self.posts()], [self.keys[1]])
        self.used[self.keys[1]] = 1000
        with self.assertRaises(SearchUnavailable):
            self.router().search('second', 'tavily')
        self.assertEqual(len(self.posts()), 1)

    def test_usage_unknown_fails_closed(self):
        self.errors = {(key, 'usage'): 500 for key in self.keys}
        with self.assertRaises(SearchUnavailable):
            self.router().search('test', 'tavily')
        self.assertEqual(self.posts(), [])
        for payload in [{}, {'account': {'plan_usage': 1, 'plan_limit': None}},
                        {'account': {'plan_usage': float('nan'), 'plan_limit': 1000}},
                        {'account': {'plan_usage': 0, 'plan_limit': 1000},
                         'key': {'usage': 10, 'limit': 10}}]:
            self.assertFalse(SearchRouter._within_free_quota(payload))

    def test_search_failure_switches_account_without_secret_in_error(self):
        self.errors[(self.keys[0], 'search')] = RuntimeError(self.keys[0])
        self.assertEqual(self.router().search('test', 'tavily'), ROWS)
        self.assertEqual([x[0] for x in self.posts()], self.keys)
        state = json.loads((self.data / 'tavily-state.json').read_text())
        self.assertEqual(sorted(state['attempts'].values()), [1, 1])
        self.errors[(self.keys[1], 'search')] = RuntimeError(self.keys[1])
        with self.assertRaises(SearchUnavailable) as raised:
            self.router().search('next', 'tavily')
        self.assertTrue(all(key not in str(raised.exception) for key in self.keys))

    def test_persisted_attempt_limit_and_monthly_reset(self):
        router = self.router()
        router.search('first', 'tavily')
        path = self.data / 'tavily-state.json'
        state = json.loads(path.read_text())
        state['attempts'] = dict.fromkeys(state['attempts'], 1000)
        path.write_text(json.dumps(state))
        self.used[self.keys[1]] = 1000
        with self.assertRaises(SearchUnavailable):
            self.router().search('second', 'tavily')
        self.current_month = '2026-11'
        self.router().search('third', 'tavily')
        self.assertEqual(len(self.posts()), 2)

    def test_concurrent_identical_requests_spend_once(self):
        router = self.router()
        barrier = threading.Barrier(3)
        results = []
        def run():
            barrier.wait()
            results.append(router.search('identical', 'tavily'))
        threads = [threading.Thread(target=run) for _ in range(2)]
        for thread in threads:
            thread.start()
        barrier.wait()
        for thread in threads:
            thread.join()
        self.assertEqual(len(results), 2)
        self.assertEqual(len(self.posts()), 1)

    def test_invalid_configuration_does_not_send_requests(self):
        (self.data / 'tavily-keys.json').write_text('invalid')
        with self.assertRaises(SearchUnavailable):
            self.router().search('test', 'tavily')
        self.assertEqual(self.calls, [])


if __name__ == '__main__':
    unittest.main()
