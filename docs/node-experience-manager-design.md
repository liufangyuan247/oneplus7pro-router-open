# Node Experience Manager Design

Status: implementation specification
Owner: Router Admin
Implementation agent: Antigravity
Review owner: Codex
Target repository: `oneplus7pro-router-open`

## 1. Summary

Add a first-party **Node Experience Manager** to the existing Router Admin
Mihomo page. It supplements latency-only health checks with bounded,
repeatable throughput measurements that are representative of Google and
YouTube delivery. It ranks nodes for video playback, recommends a winner, and
applies that winner only after explicit confirmation.

Do not replace Mihomo, expose its controller, or remove MetaCubeXD.
MetaCubeXD remains an authenticated advanced diagnostics page. The first-party
manager becomes the normal interface for selecting household nodes.

The implementation must isolate tests from production traffic by introducing
a dedicated Mihomo selector and loopback-only listener:

```text
Router Admin benchmark worker
        |
        v
127.0.0.1:17894 (node-benchmark listener)
        |
        v
家庭测速 (test-only selector)
        |
        v
one subscription node at a time

家庭RULE continues carrying household traffic throughout the test.
```

## 2. Problem statement

The current `家庭RULE自动` group uses Mihomo `url-test` with
`https://www.gstatic.com/generate_204`. That endpoint has no response body and
therefore measures reachability and response latency, not sustained bandwidth.

Observed on 2026-09-13:

- `香港 08` reported about 141 ms but sustained only about 2.5 Mbit/s.
- `日本07 | 直连 | 三网优化` reported about 260 ms but sustained
  43.5-64.6 Mbit/s over three runs.
- YouTube routed correctly through `家庭RULE`, yet its GoogleVideo transfer
  stayed near 2.1-6.7 Mbit/s on `香港 08`, which was insufficient for 1440p.
- Pointing Mihomo's delay API at an 8 MB file still returned after response
  establishment rather than timing a complete download. A larger `url-test`
  URL is therefore not a valid throughput benchmark.

The node name segment `直连` is provider-supplied text. It must not be treated
as Mihomo's `DIRECT` policy value.

## 3. Goals

1. Measure real payload throughput through individual subscription nodes.
2. Keep household traffic on the current `家庭RULE` selection while testing.
3. Rank nodes for stable 1440p/1440p60 playback, not only low RTT.
4. Make traffic cost, test progress, failures, and result age visible.
5. Require explicit user action before changing the production selector in
   the initial release.
6. Preserve Router Admin authentication and the loopback-only Mihomo
   controller boundary.
7. Persist completed results so a page refresh or Router Admin restart does
   not erase the last useful ranking.
8. Keep MetaCubeXD available as an advanced view.

## 4. Non-goals

- Reimplementing all MetaCubeXD pages.
- Replacing the Mihomo core.
- Exposing ports 9091 or 17894 to the LAN.
- Testing LAN Wi-Fi performance. Benchmarks run on the phone so that node/WAN
  performance is isolated from the client Wi-Fi link.
- Continuously downloading test payloads every five minutes.
- Treating latency as a direct proxy for throughput.
- Automatically editing subscription-provided node definitions.
- Using arbitrary user-provided benchmark URLs.

## 5. Product decision

Implement this feature inside Router Admin. Do not create a separate daemon or
separate authentication surface.

Reasons:

- Router Admin already authenticates users and proxies safe Mihomo operations.
- Existing APIs already list groups, measure delay, switch selectors, and list
  connections.
- The backend already owns persistent router state under `router-data`.
- A second service would duplicate authentication, process supervision,
  controller access, UI deployment, and rollback behavior.
- MetaCubeXD is still useful for generic rule, connection, and log inspection.

In the UI, relabel the current action from `节点测速` to `测延迟`, because its
result is not a bandwidth measurement. Add a separate `真实体验测速` card.

## 6. Mihomo configuration changes

### 6.1 Test selector

Add a test-only selector to both the checked-in base configuration and the
subscription adaptation generator:

```yaml
proxy-groups:
  - name: 家庭测速
    type: select
    include-all-proxies: true
    exclude-filter: "(?i)剩余|到期|官网|重置"
```

The generated group must contain individual subscription proxies, not
`DIRECT`, `REJECT`, or other proxy groups.

### 6.2 Test listener

Add a dedicated loopback-only mixed listener:

```yaml
listeners:
  - name: node-benchmark
    type: mixed
    listen: 127.0.0.1
    port: 17894
    proxy: 家庭测速
    udp: true
```

Requirements:

- Port 17894 must never bind to `0.0.0.0`.
- The normal `mixed-port` remains 17890.
- CPA/AI remains 17891.
- Antigravity remains 17892.
- LAN TProxy remains 17893.
- Configuration generation must preserve all existing listeners.
- Subscription refreshes must recreate `家庭测速` and `node-benchmark`.
- The Router Admin normal proxy-group selector should hide `家庭测速`; it is
  an implementation detail. MetaCubeXD may still display it.

### 6.3 Runtime measurement consistency

Ensure generated runtime configuration explicitly contains:

```yaml
unified-delay: true
tcp-concurrent: true
```

This improves comparability of latency results but does not replace throughput
testing. Do not change `家庭RULE自动` into a bandwidth selector.

## 7. Benchmark targets

The initial allowlisted target is:

```text
https://dl.google.com/android/repository/platform-tools-latest-windows.zip
```

Rationale:

- HTTPS and stable hostname.
- Served from Google infrastructure, making it more representative of
  Google/YouTube routing than a generic connectivity endpoint.
- Large enough for a bounded partial read.
- Does not require credentials or expose user data.

Implementation requirements:

- The URL is a backend constant, not browser input.
- Send `Range: bytes=0-(N-1)` for the requested sample size.
- Accept HTTP 200 or 206.
- Even if the server ignores Range, read at most N bytes and close the body.
- Disable transparent compression.
- Create a fresh transport/connection for every run; do not reuse a connection
  established through a previously selected node.
- Record DNS/connect/TLS/TTFB when available, but calculate payload throughput
  from bytes read and body-read elapsed time.
- A run is successful only if at least 90% of the requested bytes are read.
- Enforce connect, TLS-handshake, response-header, body-idle, per-run, and
  total-job timeouts. Any timeout must cancel/close the active request and
  retain the bytes received before interruption.

Do not use `generate_204` to calculate throughput. It remains suitable only for
reachability and latency screening.

## 8. Candidate discovery

Read candidates from the Mihomo controller's `家庭测速` group.

Exclude:

- `DIRECT` and `REJECT` by exact value.
- Known group names returned by `/proxies`.
- Empty names.
- Provider metadata matching `剩余|到期|官网|重置`.
- Nodes that fail the preliminary delay/reachability test.

Never exclude a node merely because its provider-supplied name contains the
text `直连`.

Take a candidate fingerprint at job start: ordered candidate names plus the
active subscription identity if available. If a subscription refresh changes
the group during a job, abort with `configuration_changed` rather than applying
a stale recommendation.

## 9. Test modes

### 9.1 Quick test

Purpose: routine selection before video playback.

1. Measure reachability/latency for all candidates.
2. Always include the current `家庭RULE` concrete node when it is a candidate.
3. Build a shortlist of at most 10 nodes using regional quotas so the list is
   not dominated by one location:
   - up to 3 Hong Kong nodes;
   - up to 2 Japan nodes;
   - up to 2 Taiwan/Singapore nodes;
   - up to 2 other healthy nodes;
   - current node if not already included.
4. Run one 3 MiB measurement for every shortlisted node.
5. Take the top three by preliminary effective speed.
6. Run two additional 8 MiB confirmation measurements per finalist.
7. Interleave finalists and randomize order within each round.

Hard limits:

- 100 MiB for the whole job;
- 25 MiB accumulated per node;
- 10 minutes wall-clock time for the whole job.

Stop before starting a run whose requested bytes would exceed either byte
budget. If a partial run is interrupted, count and persist the bytes already
received.

### 9.2 Deep test

Purpose: new subscription, widespread degradation, or manual diagnostics.

1. Test every healthy individual node once with 4 MiB.
2. Retest the top five twice with 8 MiB.
3. Interleave and randomize confirmation rounds.

Hard limits:

- 300 MiB for the whole job;
- 35 MiB accumulated per node;
- 30 minutes wall-clock time for the whole job.

Stop before starting a run whose requested bytes would exceed either byte
budget. If a partial run is interrupted, count and persist the bytes already
received.

### 9.3 Per-run time limits

Every HTTP sample uses the following hard ceilings:

- TCP connect: 5 seconds;
- TLS handshake: 5 seconds;
- response headers: 8 seconds;
- no body progress: 5 seconds;
- entire sample request: 30 seconds.

The body-idle timer resets only after payload bytes are received. Reaching any
ceiling must close the response body and release the connection promptly; a
slow or stalled node must never hold the worker until the job-wide deadline.

### 9.4 Cancellation

Cancellation must:

- cancel the current HTTP request through context cancellation;
- restore the initial `家庭测速` selection when possible;
- leave `家庭RULE` unchanged;
- persist the job as `cancelled` with bytes already consumed;
- release the single-job lock.

## 10. Scoring and recommendation

For confirmed finalists, calculate:

```text
median_mbps = median(successful confirmation runs)
minimum_mbps = minimum(successful confirmation runs)
effective_mbps = 0.70 * minimum_mbps + 0.30 * median_mbps
```

Preliminary runs may be displayed but must not be mixed with confirmation runs
of a different sample size when calculating the final effective score.

Recommendation ordering:

1. Eligible before ineligible.
2. Higher `effective_mbps`.
3. Higher `minimum_mbps`.
4. Lower median TTFB.
5. Lower median delay.
6. Stable node-name ordering as the final deterministic tie breaker.

Eligibility requires:

- at least two successful confirmation runs;
- zero hard connection failures in confirmation;
- `minimum_mbps >= 8`.

Video badges use conservative thresholds based on minimum confirmed speed:

| Minimum confirmed speed | Badge |
|---:|---|
| `< 8 Mbit/s` | unstable / below 1080p target |
| `8-14.99 Mbit/s` | 1080p |
| `15-24.99 Mbit/s` | 1440p |
| `25-39.99 Mbit/s` | 1440p60 |
| `>= 40 Mbit/s` | 4K candidate |

Latency is a tie breaker, never the primary score.

## 11. Applying a result

The initial release is manual-apply only.

Before applying:

- the job must be `completed`;
- the requested node must be an eligible result from that job;
- the candidate fingerprint must still match;
- the node must still belong to `家庭RULE`;
- Mihomo must be online.

Apply with the controller selector API for `家庭RULE`. Return and display the
old and new selections. Do not close existing connections automatically; the
UI should explain that an already-established YouTube stream may need reload
or reconnection before using the new node.

An optional later release may offer automatic apply with all of these guards:

- winner effective speed is at least 25% and 10 Mbit/s above current;
- winner passes two consecutive jobs;
- minimum hold time is 30 minutes;
- no deep test is scheduled more than once per day;
- an explicit user setting enables automatic apply.

Automatic apply is out of scope for the first implementation.

## 12. Background-load contamination

The benchmark runs on the phone and therefore excludes client Wi-Fi, but it
shares the WAN with household traffic.

Before the first download, sample WAN traffic for three seconds. If non-test
traffic exceeds 5 Mbit/s:

- quick mode may continue but mark the job `contaminated: true`;
- deep mode should refuse to start unless the request explicitly includes
  `allowBusyNetwork: true`;
- the UI must show that results may be lower because the WAN was already busy.

Run node downloads sequentially. Never benchmark multiple nodes concurrently;
parallel tests would measure contention created by the benchmark itself.

Do not pause or terminate user downloads automatically in the initial release.

## 13. Backend state model

Add a single asynchronous benchmark worker to the existing Go process.

Suggested server fields:

```go
speedTestMu     sync.Mutex
speedTestJob    *MihomoSpeedTestJob
speedTestCancel context.CancelFunc
speedTestHTTP   func(...) // injectable seam for tests
```

Only one job may run at a time. A second start request returns HTTP 409 with
the current job ID.

Suggested public model:

```json
{
  "id": "20260913T110500Z-ab12cd",
  "mode": "quick",
  "state": "running",
  "phase": "confirming",
  "startedAt": "2026-09-13T11:05:00+08:00",
  "finishedAt": "",
  "currentNode": "日本07 | 直连 | 三网优化",
  "completedRuns": 8,
  "totalRuns": 14,
  "bytesUsed": 50331648,
  "dataBudgetBytes": 104857600,
  "contaminated": false,
  "initialProductionNode": "香港 08",
  "recommendedNode": "日本07 | 直连 | 三网优化",
  "candidateFingerprint": "sha256:...",
  "results": []
}
```

Each node result contains:

```json
{
  "node": "日本07 | 直连 | 三网优化",
  "protocol": "Vless",
  "delayMs": 260,
  "runs": [
    {
      "stage": "confirmation",
      "requestedBytes": 8388608,
      "receivedBytes": 8388608,
      "durationMs": 1490,
      "ttfbMs": 410,
      "mbps": 45.0,
      "httpStatus": 206,
      "success": true,
      "errorCode": ""
    }
  ],
  "medianMbps": 45.0,
  "minimumMbps": 43.5,
  "effectiveMbps": 44.0,
  "eligible": true,
  "videoBadge": "1440p60"
}
```

Use stable machine-readable error codes such as:

- `mihomo_offline`
- `benchmark_group_missing`
- `benchmark_listener_unavailable`
- `configuration_changed`
- `connect_timeout`
- `header_timeout`
- `body_timeout`
- `short_read`
- `unexpected_status`
- `cancelled`
- `data_budget_exceeded`
- `node_budget_exceeded`
- `job_timeout`
- `busy_network`

Do not expose raw subscription URLs, node credentials, controller responses,
or internal stack traces.

## 14. HTTP API

All routes use the existing Router Admin authentication wrapper.

### `GET /api/mihomo/speedtest`

Return the active job, or the most recent completed/cancelled job. Return a
stable empty state when no job has ever run.

### `POST /api/mihomo/speedtest`

Request:

```json
{
  "mode": "quick",
  "allowBusyNetwork": false
}
```

Response: HTTP 202 with the initial job state. Accepted modes are `quick` and
`deep`. Reject unknown fields and excessive request bodies using existing JSON
helpers.

### `DELETE /api/mihomo/speedtest`

Cancel the active job. Return HTTP 409 if no job is running.

### `PUT /api/mihomo/speedtest/apply`

Request:

```json
{
  "jobId": "20260913T110500Z-ab12cd",
  "node": "日本07 | 直连 | 三网优化"
}
```

Return:

```json
{
  "ok": true,
  "oldNode": "香港 08",
  "newNode": "日本07 | 直连 | 三网优化"
}
```

The implementation may split handlers internally, but the external contract
must remain explicit and authenticated.

## 15. Persistence

Store completed and cancelled jobs at:

```text
~/router-data/mihomo/speedtest-results.json
```

Requirements:

- atomic write using a same-directory temporary file and rename;
- file mode 0600;
- schema version field;
- retain at most the latest 10 jobs;
- never persist contexts, credentials, subscription URLs, or raw response
  headers;
- on startup, convert a persisted `running` job to `interrupted`;
- malformed persistence must not prevent Router Admin startup.

## 16. Frontend design

Add the card below `代理组与节点` on the existing Mihomo page.

### 16.1 Header

- Title: `真实体验测速`
- Explanatory text: `在手机路由器本机下载测试数据，不经过电脑 Wi-Fi。`
- Buttons: `快速测速`, `深度测速`, and `停止` while running.
- Show estimated maximum traffic before the user starts a job.

### 16.2 Progress

Show:

- current phase and node;
- completed/total runs;
- bytes consumed;
- elapsed time;
- busy-network warning;
- cancellation state.

Poll once per second only while a job is active. When idle, load the most
recent result once with the rest of the Mihomo page.

### 16.3 Results table

Columns:

- recommendation marker;
- node;
- protocol;
- delay;
- individual run speeds;
- minimum speed;
- effective speed;
- stability/failure state;
- video badge;
- `应用` button.

Default sorting is recommendation order. Failed nodes remain visible below
successful nodes with a concise error label.

### 16.4 Selection semantics

- Highlight the current production `家庭RULE` node separately from the
  recommended node.
- Never imply that a recommendation has already been applied.
- After apply, update the existing proxy group UI state.
- Explain that existing long-lived streams may need a reload.
- Keep the `打开 MetaCubeXD` button and label it as an advanced view.

### 16.5 Compatibility

- Preserve responsive mobile layout.
- Preserve the existing Chrome 87 export/transpile workflow.
- Avoid adding a heavy charting dependency; use native tables, bars, and CSS.
- All controls must have text labels, disabled states, and useful error text.

## 17. Security requirements

- Mihomo controller remains `127.0.0.1:9091` only.
- Benchmark listener remains `127.0.0.1:17894` only.
- Every new browser API uses existing Router Admin authentication and
  same-origin protection.
- Test URL and port are backend constants.
- Node names are passed as JSON to the controller; never interpolate them into
  a shell command.
- Limit node-name length and reject invalid UTF-8/control characters.
- Limit job history size, response size, and request body size.
- Never return subscription URLs or node credentials.
- A benchmark must not invoke `su`, modify firewall rules, or restart Mihomo.
- Applying a result is the only operation allowed to change `家庭RULE`.

## 18. Concurrency and failure handling

- One benchmark job at a time.
- One node download at a time.
- A Mihomo restart aborts the job without changing `家庭RULE`.
- A subscription/group change aborts the job as `configuration_changed`.
- A Router Admin restart leaves the last completed result readable.
- A client closing the browser does not cancel the server-side job.
- HTTP client contexts must prevent leaked goroutines and sockets.
- Always restore the initial `家庭测速` selection on completion, cancellation,
  or failure when the group is still available.
- Never restore or modify `家庭RULE` because the benchmark never changes it.

## 19. Tests

### 19.1 Go unit tests

Add tests for:

- candidate filtering, including provider names containing `直连`;
- regional shortlist quotas;
- median/minimum/effective score calculation;
- deterministic tie breaking;
- video badge thresholds;
- failed/partial/timeout run classification;
- total and per-node data-budget enforcement, including proof that a blocked
  request was not sent;
- connect/TLS/header/body-idle/per-run/job timeout interruption against a real
  local HTTP server where practical;
- partial-byte accounting and persistence after timeout/cancellation;
- single-job HTTP 409 behavior;
- cancellation;
- stale fingerprint rejection on apply;
- apply validation and successful selector update;
- persistence atomicity and malformed-file recovery;
- redaction/no-secret API responses;
- benchmark group hidden from normal group listing.

Use an injectable HTTP measurement seam and a fake Mihomo controller. Tests
must not access the public internet.

### 19.2 Configuration tests

Verify generated configuration contains exactly one:

- `家庭测速` selector;
- `node-benchmark` listener;
- loopback bind for port 17894;
- preserved AI, Antigravity, and LAN listeners;
- `unified-delay: true` and `tcp-concurrent: true`.

Validate both base config and an adapted subscription candidate with
`mihomo -t` when the binary is available.

### 19.3 Frontend verification

Run:

```text
npm run lint
npm run export
```

Verify:

- idle, running, completed, cancelled, failed, and contaminated states;
- narrow mobile layout;
- progress polling stops after terminal state;
- apply button cannot use stale or ineligible results;
- Chrome 87 compatibility output is still generated.

### 19.4 Repository checks

Run:

```text
cd admin/backend && go test ./...
sh -n services/mihomo/update-subscription.sh
```

Do not deploy to the phone as part of automated tests.

## 20. Deployment and rollback

Implementation and deployment are separate approvals.

Deployment sequence:

1. Build and test the backend and frontend locally.
2. Generate a subscription candidate with validation-only mode.
3. Validate the candidate with Mihomo.
4. Back up the active Mihomo config, Router Admin binary, and web assets.
5. Deploy configuration generator and Router Admin artifacts.
6. Refresh the subscription/config and restart Mihomo once.
7. Verify listeners 17890-17894, controller isolation, TUN/TProxy state, and
   existing CPA/Antigravity routes.
8. Run a quick benchmark without applying its result.
9. Confirm `家庭RULE` stayed unchanged during the job.
10. Apply a result manually and verify a new YouTube connection uses it.

Rollback must restore the previous Router Admin artifacts and Mihomo config,
then verify ports 17890-17893 and controller health. Removing the feature must
not alter subscriptions or the current `家庭RULE` selection.

## 21. Acceptance criteria

The implementation is complete only when all are true:

1. A quick test can run from the authenticated Router Admin page.
2. Household `家庭RULE` selection is unchanged for the full duration of the
   benchmark.
3. Traffic goes through loopback port 17894 and the selected `家庭测速` node.
4. The UI reports progress and bytes consumed.
5. The results distinguish delay from payload throughput.
6. Three-run finalist scoring uses minimum and median throughput.
7. The user can explicitly apply an eligible result.
8. Applying updates the existing proxy-group UI and Mihomo selection.
9. Cancellation leaves production routing unchanged.
10. Results survive page refresh and Router Admin restart.
11. Controller and benchmark listener remain inaccessible from the LAN.
12. Existing Mihomo, CPA, Antigravity, TUN, LAN TProxy, subscription, logs,
    connections, and MetaCubeXD features continue working.
13. Backend tests, frontend lint/export, shell syntax checks, and config
    validation pass.
14. No deployment is performed until Codex review accepts the implementation.

## 22. Implementation boundaries for Antigravity

Antigravity should implement the specification and tests in the repository,
but must not:

- deploy to the phone;
- restart services;
- update the live subscription;
- switch the live `家庭RULE` node;
- remove MetaCubeXD;
- broaden listener binds;
- weaken authentication;
- commit or push changes.

When finished, Antigravity must provide:

1. a concise changed-file summary;
2. test commands and complete results;
3. known limitations;
4. confirmation that no live deployment or service mutation occurred;
5. any design deviations requiring reviewer approval.
