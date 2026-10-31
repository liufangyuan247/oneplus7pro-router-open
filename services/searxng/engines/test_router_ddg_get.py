"""Parser checks; no live search requests."""
import unittest
from types import SimpleNamespace

from searx.exceptions import SearxEngineCaptchaException, SearxEngineTooManyRequestsException
from router_ddg_get import parse_results, response


class DuckDuckGoGetParserTests(unittest.TestCase):
    def test_redirect_is_decoded_once_and_snippet_is_preserved(self):
        result = parse_results('''<div class="result web-result"><h2>
          <a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.org%2Fa%253Fb">A <b>title</b></a>
          </h2><a class="result__snippet">Some <b>text</b>.</a></div>''')[0]
        self.assertEqual(result['url'], 'https://example.org/a%3Fb')
        self.assertEqual(result['title'], 'A title')
        self.assertEqual(result['content'], 'Some text.')

    def test_captcha_is_not_reported_as_empty_search(self):
        for body in ['<form id="challenge-form"></form>', '<script src="/anomaly.js"></script>', '<div class="anomaly-modal"></div>']:
            with self.subTest(body=body), self.assertRaises(SearxEngineCaptchaException):
                parse_results(body)

    def test_unsafe_urls_ads_duplicates_and_result_limit(self):
        body = '''<a class="result__a" href="javascript:alert(1)">bad</a>
          <div class="result--ad"><a class="result__a" href="https://ads.example/">advert</a></div>
          <a class="result__a" href="https://example.org/">first</a>
          <a class="result__a" href="https://example.org/">duplicate</a>
          <a class="result__a" href="https://example.net/">second</a>'''
        self.assertEqual([x['title'] for x in parse_results(body)], ['first', 'second'])
        self.assertEqual(len(parse_results(body, limit=1)), 1)

    def test_no_results_and_rate_limit_are_distinct(self):
        self.assertEqual(parse_results('<html><body>No results</body></html>'), [])
        with self.assertRaises(SearxEngineTooManyRequestsException):
            response(SimpleNamespace(status_code=429, content=b''))

    def test_text_about_captchas_is_not_a_challenge(self):
        result = parse_results('<a class="result__a" href="https://example.org/">What is anomaly.js?</a>')
        self.assertEqual(len(result), 1)


if __name__ == '__main__':
    unittest.main()
