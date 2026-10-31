# DuckDuckGo HTML GET for an agent

The `duckduckgo html get` engine follows the public HTML reader approach
provided by the user. It is a separate engine extension in
`engines/router_ddg_get.py`; no existing upstream SearXNG engine file is edited.
It reuses the already-installed `curl_cffi` HTTP backend and `lxml` parser.
There is no additional service process or HTTP library installation.

## Request and result handling

| Item | Implementation |
| --- | --- |
| Upstream URL | `https://html.duckduckgo.com/html/` |
| Method | GET with URL-encoded `q`; first page only |
| User-Agent | `RouterSearch/1.0 (public source reader)` |
| Browser impersonation | Explicitly disabled for this extension |
| Proxy | Uses the instance's explicit outgoing configuration |
| Rate | At least 2 seconds between request starts across WSGI threads |
| Results | At most 10 organic results; title, original URL and snippet |
| Redirect URLs | Decode DuckDuckGo's `uddg` parameter once |
| Dates | No publication date is invented |
| CAPTCHA | Recognize challenge form, anomaly modal or anomaly script; report CAPTCHA with a 300-second suspension request |
| Rate limit | Report upstream HTTP 429 separately; do not loop or rotate proxies |

The parser filters ads, duplicate URLs and non-HTTP(S) URLs. It does not fetch
linked pages. A separate page-reader tool should validate destinations before
fetching and apply its own protections against accessing private networks.

The parser extension itself does not implement result caching or fallback.
The separate default `auto` adapter adds a 300-second, 64-query cache and sequential
Tavily fallback using private keys. It pauses the primary for 300 seconds after
an exception. Selecting this free engine explicitly retains native SearXNG
suspension handling. Neither path guarantees CAPTCHA avoidance.

## Why this differs from the upstream engines

The pinned upstream `duckduckgo` engine submits an HTML POST form with extra
parameters, region settings and browser-style headers. The separate
`duckduckgo web` engine obtains a first-page link and requests the `d.js` JSON
endpoint. Neither is the same request as a minimal HTML GET.

Matched low-frequency tests on the router found:

| Request | Observation |
| --- | --- |
| Minimal GET without an explicit HTTP proxy | HTTP 200, 10 relevant results |
| Minimal GET through the router HTTP proxy | HTTP 200, 10 relevant results |
| Minimal POST through the same proxy | HTTP 200, 10 relevant results |
| Unmodified SearXNG HTML engine, same query | CAPTCHA |
| SearXNG HTML engine with only the User-Agent changed | CAPTCHA |

The HTTP client used for these comparisons was `curl_cffi` with no browser
impersonation, not the other project's `httpx`. A minimal request without an
explicit proxy is not proof of a different public egress: router-level routing
may still apply. These observations do not isolate one cause. In particular,
neither GET alone nor a User-Agent string should be described as a CAPTCHA bypass.

The installed extension, using the honest RouterSearch User-Agent, returned 10
English and 10 Chinese results in approximately 1.3 and 1.7 seconds. Five parser
unit tests cover redirect decoding, snippets, CAPTCHA and rate-limit errors,
ad/duplicate filtering, result limits and false CAPTCHA markers in normal text.

## Connect an agent

Use the existing instance; no provider API key is required:

```sh
curl -G 'http://ROUTER_LAN_ADDRESS:8888/search' \
  --data-urlencode 'q=Python programming official documentation' \
  --data-urlencode 'engines=duckduckgo html get' \
  --data 'format=json'
```

When `engines` is omitted, `auto` tries this extension first, then the private
Tavily pool on failure/empty results. Use `engines=auto` explicitly if a client
has old preferences. Map the JSON
`results[].title`, `results[].url` and `results[].content` fields into the agent's
title, URL and snippet fields. Select the first 5-10 results.

Check `unresponsive_engines` even if the service returned HTTP 200. An upstream
CAPTCHA, timeout, access denial or rate limit is different from a successful
search with no results. If only partial results are present, preserve the
warnings. Read the linked original pages separately when the agent needs evidence
or citations beyond the search snippets.
