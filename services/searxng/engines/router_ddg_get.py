"""Low-volume, first-page DuckDuckGo HTML GET adapter for SearXNG.

This reads the public no-JavaScript search page. It does not solve CAPTCHAs.
Keep this extension separate from upstream's duckduckgo.py implementation.
"""
import threading
import time
from urllib.parse import parse_qs, urlencode, urljoin, urlsplit

from lxml import html
from searx.exceptions import SearxEngineCaptchaException, SearxEngineTooManyRequestsException

about = {
    'website': 'https://html.duckduckgo.com/html/',
    'use_official_api': False,
    'require_api_key': False,
    'results': 'HTML',
}
categories = ['general']
paging = False
language_support = False
time_range_support = False
max_results = 10
minimum_interval = 2.0
base_url = 'https://html.duckduckgo.com/html/'
_request_lock = threading.Lock()
_last_request = 0.0


def request(query, params):
    global _last_request
    if not query.strip() or len(query) >= 500:
        params['url'] = None
        return
    # One request start per interval across all WSGI threads. No retry loop.
    with _request_lock:
        delay = minimum_interval - (time.monotonic() - _last_request)
        if delay > 0:
            time.sleep(delay)
        _last_request = time.monotonic()
    params['url'] = base_url + '?' + urlencode({'q': query})
    params['method'] = 'GET'
    params['data'] = {}
    params['cookies'] = {}
    params['impersonate'] = None
    params['default_headers'] = False
    params['headers'] = {
        'User-Agent': 'RouterSearch/1.0 (public source reader)',
        'Accept': 'text/html',
    }


def _has_class(value):
    return 'contains(concat(" ",normalize-space(@class)," ")," ' + value + ' ")'


def _text(nodes):
    return ' '.join(' '.join(''.join(node.itertext()) for node in nodes).split())


def parse_results(body, limit=10):
    raw = body.encode() if isinstance(body, str) else body
    if not raw.strip():
        return []
    dom = html.fromstring(raw)
    if dom.xpath('//form[@id="challenge-form" or contains(@action,"anomaly.js")]'
                 ' | //*[' + _has_class('anomaly-modal') + ']'
                 ' | //script[contains(@src,"anomaly.js")]'):
        raise SearxEngineCaptchaException(suspended_time=300)
    results, seen = [], set()
    for link in dom.xpath('//a[' + _has_class('result__a') + ']'):
        if link.xpath('ancestor::*[' + _has_class('result--ad') + ']'):
            continue
        url = urljoin(base_url, link.get('href') or '')
        try:
            parsed = urlsplit(url)
        except ValueError:
            continue
        host = (parsed.hostname or '').lower()
        if host == 'duckduckgo.com' or host.endswith('.duckduckgo.com'):
            target = parse_qs(parsed.query).get('uddg', [])
            if target:
                url = target[0]
        try:
            parsed = urlsplit(url)
        except ValueError:
            continue
        if parsed.scheme not in ('http', 'https') or not parsed.hostname:
            continue
        title = _text([link])
        if not link.get('href') or not title or url in seen:
            continue
        containers = link.xpath('ancestor::*[' + _has_class('result') + '][1]')
        snippets = containers[0].xpath('.//*[' + _has_class('result__snippet') + ']') if containers else []
        results.append({'title': title, 'url': url, 'content': _text(snippets)})
        seen.add(url)
        if len(results) >= limit:
            break
    return results


def response(resp):
    if resp.status_code == 429:
        raise SearxEngineTooManyRequestsException(suspended_time=300)
    return parse_results(resp.content, max_results)
