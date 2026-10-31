# SearXNG on Termux

Self-hosted search UI and HTTP JSON API, with free HTML search first and an
optional private Tavily basic fallback.
Runs the official SearXNG source in a native Android Python virtual environment.
No container, Linux distribution, browser automation, or Valkey daemon is needed.

## Runtime

- Upstream: <https://github.com/searxng/searxng>
- Pinned revision: `ffa88e3ba3ade64185d5754c839cdf5a80708c4a` (2026-10-30).
- Source archive SHA-256: `a3b50a7eb4bc18b048ba1dc7898485e5e358dc8ea14b034264da0f27762f2e25`.
- Native Termux Python 3.14, `libxml2`, `libxslt`, `libffi`, and a C compiler.
- Official pinned Python requirements, plus Waitress 3.0.2 as a single-process
  WSGI server with two request threads and at most 16 connections.
- `requirements.lock.txt` records the complete tested runtime dependency set.
- The default `auto` extension first calls `duckduckgo html get`. It reads
  only the first public HTML results page using GET, without browser automation.
  Brave's free web-search engine, Yahoo, DuckDuckGo Web and 360
  are also loaded, but disabled for default searches. Request them explicitly
  with `engines=` or enable them in the web preferences when needed. Default
  requests normally query only DuckDuckGo. A failure or empty result invokes
  Tavily if private keys are configured. CAPTCHA/network failures pause the
  primary for 300 seconds. Explicit `engines=tavily` skips DuckDuckGo.
- Tavily keys rotate per uncached request; an unavailable or exhausted key is
  skipped. This is round-robin scheduling, not parallel fanout. Duplicate keys
  are removed. Keys belonging to the same account still share its quota.
- Default-source results: up to 10; Tavily fallback/explicit: up to 5.
  Each mode caches up to 64 queries for 300 seconds in process memory. One
  lock prevents concurrent spending; callers waiting over 0.5 seconds receive
  a busy error. The orchestration deadline is 20 seconds (engine timeout 23).
- Outgoing connection pool: 4. The sequential adapter reuses one synchronous
  `curl_cffi` session through `SEARXNG_SEARCH_PROXY` (default
  `http://127.0.0.1:17890`); changing the SearXNG YAML proxy alone does not change
  this adapter's proxy. Metrics and image proxy are disabled.
- Searches run on demand. No scheduled queries or crawling are configured.

`curl_cffi` provides an Android wheel; the other native Python extensions may
need compilation. `prepare-termux.sh` limits build parallelism to one job.
Use low-priority build execution on the phone; compilation cost is temporary
and should not be confused with the steady-state service resource use.

## Install

Run as the Termux user, using the existing router-stack runit supervisor:

1. Stage this directory's files under `~/.stage/searxng-install/`, including
   the three `engines/router_*.py` modules staged flat, plus `router_search.py`.
2. Download the pinned upstream source to that directory as `source.tar.gz`:
   `https://codeload.github.com/searxng/searxng/tar.gz/ffa88e3ba3ade64185d5754c839cdf5a80708c4a`.
3. Prepare and activate:

```sh
nice -n 15 taskset -c 0-3 sh ~/.stage/searxng-install/prepare-termux.sh \
  a3b50a7eb4bc18b048ba1dc7898485e5e358dc8ea14b034264da0f27762f2e25
python ~/.stage/searxng-install/activate-termux.py --bind 127.0.0.1
```

Choose the router's LAN address for `--bind` when other devices need access.
Activation is for a first install and stops the new service if its health check
fails. It does not restart other services. Future updates need a separate
candidate environment and a tested release swap.

The scripts generate upstream-supported `version_frozen.py` archive metadata
and install the separate `router_*.py` engine extensions. Existing upstream
engine and application source files remain unchanged. The extension is copied
alongside each new release, without editing upstream's `duckduckgo.py`.

## Paths and controls

- Release and isolated Python packages: `~/router-apps/searxng/releases/<commit>/`.
- Active release: `~/router-apps/searxng/current`.
- Runtime configuration: `~/router-data/searxng/settings.yml`, mode 0600.
- Listen address: `~/router-data/searxng/listen.env`, mode 0600.
- Generated local cache files: `~/router-data/searxng/cache/`.
- Service: `~/router-stack/services/searxng/`; bounded runit logs under `log/main/`.
- Router Admin: SearXNG appears in the AI services group with open, log,
  start/stop and restart actions. Automatic upgrade is deliberately unavailable
  until a transactional dependency upgrade path is implemented.

The generated Flask secret stays on the device. Do not commit live settings or
log files. Restrict the unauthenticated search UI
and API to trusted local clients.

## Optional Tavily configuration

Keep real keys in `~/router-data/searxng/tavily-keys.json` (mode 0600), never in
the public repository or YAML settings. Example structure with placeholders:

```json
{"keys":[{"name":"account-1","api_key":"tvly-REPLACE_ONE"},{"name":"account-2","api_key":"tvly-REPLACE_TWO"}]}
```

Tavily always uses `search_depth=basic`, `auto_parameters=false`, five results,
no generated answer and no raw page contents. Before each uncached search,
`GET /usage` must show enough plan and key credits; unknown usage fails closed.
The adapter caps usable plan credits at 1,000/month and keeps a separate
persistent 1,000-attempt monthly ceiling per key in `tavily-state.json` (0600).
It reserves an attempt before POST; a timeout may have consumed upstream credits
and is counted conservatively. Rotation position survives service restarts.
The usage check reflects other callers, but cannot atomically reserve credits
against separate services sharing the account. Disable pay-as-you-go in Tavily
itself for an account-level guarantee. This service never changes billing.

The key file is re-read for uncached Tavily requests. No restart is needed to
replace keys. All results and errors omit keys; persistent counters use hashes.
Usage is checked on demand, with no background quota polling.
Additional keys on one account do not grant additional credits. Separate-account
free-plan eligibility must be confirmed with Tavily; configuration alone does
not establish eligibility. See [pricing](https://www.tavily.com/pricing) and
[Platform Terms](https://www.tavily.com/terms).

## Agent integration

```sh
curl -G 'http://ROUTER_LAN_ADDRESS:8888/search' \
  --data-urlencode 'q=Python asyncio documentation' \
  --data 'format=json'
```

Read `results[].title`, `results[].url`, and `results[].content`. Agents should
select the first 5-10 useful results. A separate page-reading tool is needed
when the agent needs the full contents of a linked page. MCP is not required
for clients that support HTTP tools.

Choose an optional source with the `engines` parameter:

```sh
curl -G 'http://ROUTER_LAN_ADDRESS:8888/search' \
  --data-urlencode 'q=Python asyncio documentation' \
  --data 'format=json' --data-urlencode 'engines=duckduckgo web'
```

Other supported values are `brave`, `yahoo`, and `360search`.
The default orchestration is `auto`. Select the free source without fallback
as `duckduckgo html get`, or the rotating basic API pool as `tavily`.
Comma-separated values such as `brave,yahoo` query those sources together.
Web users can change their engine selection at `/preferences`.

At deployment time, Google and Qwant returned access denied, Baidu and
DuckDuckGo's separate HTML engine returned CAPTCHA, and Bing returned
insufficiently relevant results, including through `cn.bing.com`. Those sources
are not included in the runtime configuration. Sogou returned relevant results
once but failed during production verification, so it is also excluded.
The working `duckduckgo web`
engine uses a different endpoint from `duckduckgo`.

The default HTML GET extension and integration details are documented in
[duckduckgo-agent.md](duckduckgo-agent.md). A later verification of Brave
returned HTTP 429, so it is no longer the sole default. No free source has a
guaranteed availability or request allowance.

`/healthz` checks the local web service, not engine availability. Validate JSON
searches and result relevance separately. `unresponsive_engines` reports
upstream errors. Self-hosting does not eliminate upstream CAPTCHA, rate limits,
or network failures; test each enabled source with the actual network exit.
Clients with saved engine preferences must choose `auto` to use the new default;
API clients can send `engines=auto` explicitly. A separate page-reading tool is
still needed for full source documents.

Local routing tests: `python -m unittest -v test_router_search`.
Run the parser tests inside the installed SearXNG venv with the engines directory
on `PYTHONPATH`: `python -m unittest discover -s engines`.

## Measured native Android baseline

On a OnePlus 7 Pro running Termux Python 3.14, the initial Brave-only configuration
returned relevant English and Chinese results in approximately 0.9-2.0 seconds
for three sample queries. After those requests, RSS was 87,756 KiB (85.7 MiB),
PSS was 78,642 KiB (76.8 MiB), and 30 seconds of idle CPU sampling measured 1.13%
of one core, approximately 0.14% of the eight-core machine. These are observations,
not fixed resource limits or a latency guarantee. The native network library's
periodic timeout checker still wakes while idle; upstream library code is not
patched to remove that safeguard.
