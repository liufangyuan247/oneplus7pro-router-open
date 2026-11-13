"""Add automatic, per-pair WebRTC fallback to the pinned PairDrop release."""
import argparse
import json
from pathlib import Path
import stat

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('release', type=Path)
parser.add_argument('--check', action='store_true', help='validate without changing files')
args = parser.parse_args()

util = args.release / 'public/scripts/util.js'
worker = args.release / 'public/service-worker.js'
original = 'window.isRtcSupported = !!(window.RTCPeerConnection || window.mozRTCPeerConnection || window.webkitRTCPeerConnection);'
replacement = '''// OP7: opt-in relay mode for VPNs that block browser-to-browser WebRTC.
// Advertising no RTC support makes both ends use PairDrop's existing WS fallback.
window.isRtcSupported = !!(window.RTCPeerConnection || window.mozRTCPeerConnection || window.webkitRTCPeerConnection)
    && new URLSearchParams(window.location.search).get('transport') !== 'ws';'''
old_version = "const cacheVersion = 'v1.11.2';"
previous_version = "const cacheVersion = 'v1.11.2-op7-ws1';"
previous_auto_version = "const cacheVersion = 'v1.11.2-op7-auto-ws2';"
previous_mailbox_version = "const cacheVersion = 'v1.11.2-op7-mailbox1';"
previous_ui_version = "const cacheVersion = 'v1.11.2-op7-mailbox2';"
previous_presence_version = "const cacheVersion = 'v1.11.2-op7-mailbox3';"
previous_text_version = "const cacheVersion = 'v1.11.2-op7-mailbox4';"
new_version = "const cacheVersion = 'v1.11.2-op7-mailbox5';"
plans = []


def replace_once(path, content, old, new):
    if new in content:
        return content
    if content.count(old) != 1:
        raise SystemExit(f'Unsupported PairDrop source; refusing to patch {path.name}')
    return content.replace(old, new, 1)


plans.append((util, replace_once(util, util.read_text(encoding='utf-8'), original, replacement)))
content = worker.read_text(encoding='utf-8').replace(previous_version, old_version).replace(previous_auto_version, old_version).replace(previous_mailbox_version, old_version).replace(previous_ui_version, old_version).replace(previous_presence_version, old_version).replace(previous_text_version, old_version)
content = replace_once(worker, content, old_version, new_version)
content = replace_once(worker, content, "    'scripts/main.js',", "    'scripts/router-compat.js',\n    'scripts/main.js',")
content = replace_once(worker, content, "    'scripts/router-compat.js',",
                       "    'scripts/router-mailbox.js',\n    'styles/router-mailbox.css',\n    'scripts/router-compat.js',")
content = replace_once(worker, content, "    'manifest.json',",
                       "    'manifest.json',\n    'images/router-mailbox.svg',")
content = replace_once(worker, content,
                       "self.addEventListener('fetch', function(event) {",
                       "self.addEventListener('fetch', function(event) {\n"
                       "    // File-box requests are ordinary streaming HTTP, not Web Share Target posts.\n"
                       "    if (new URL(event.request.url).pathname.startsWith('/mailbox/api/')) {\n"
                       "        event.respondWith(fetch(event.request));\n"
                       "        return;\n"
                       "    }")
plans.append((worker, content))

index = args.release / 'public/index.html'
content = index.read_text(encoding='utf-8')
content = replace_once(index, content, '<html lang="en">',
                       '<html lang="zh-CN" class="router-mailbox-active">')
content = replace_once(index, content,
                       '<title>PairDrop | Transfer Files Cross-Platform. No Setup, No Signup.</title>',
                       '<title>设备传输站</title>')
initial_styles = '''    <!-- OP7: render the transfer-station shell before deferred scripts. -->
    <style id="router-mailbox-initial-style">
        :root { color-scheme: light; }
        html.router-mailbox-active, html.router-mailbox-active body {
            margin: 0; min-height: 100%; background: #eef3f7 !important; color: #0f172a;
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif;
        }
        html.router-mailbox-active body > header, html.router-mailbox-active body > footer,
        html.router-mailbox-active canvas.circles, html.router-mailbox-active #websocket-fallback,
        html.router-mailbox-active x-menu { display: none !important; }
        html.router-mailbox-active #center { opacity: 1 !important; }
        html.router-mailbox-active #center > :not(#router-mailbox):not(#router-mailbox-loading) {
            display: none !important;
        }
        #router-mailbox-loading {
            position: fixed; inset: 0; display: grid; place-content: center; gap: 12px;
            text-align: center; background: #eef3f7; padding: 24px; box-sizing: border-box;
        }
        #router-mailbox-loading svg { width: 48px; height: 48px; margin: auto; color: #2481cc; }
        #router-mailbox-loading h1 { margin: 0; font-size: 22px; font-weight: 600; }
        #router-mailbox-loading p { margin: 0; color: #64748b; font-size: 14px; }
        html.router-mailbox-mounted #router-mailbox-loading { display: none; }
        #router-mailbox ~ #router-mailbox-loading { display: none !important; }
    </style>
    <link id="router-mailbox-styles" rel="stylesheet" href="styles/router-mailbox.css">
'''
content = replace_once(index, content, '<head>', '<head>\n' + initial_styles)
loading = '''<div id="center" class="opacity-0">
    <section id="router-mailbox-loading" role="status" aria-live="polite" aria-label="正在打开设备传输站">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
            <path d="M22 2 11 13M22 2l-7 20-4-9-9-4 20-7Z"/>
        </svg>
        <h1>设备传输站</h1>
        <p>正在打开传输界面…</p>
    </section>'''
content = replace_once(index, content, '<div id="center" class="opacity-0">', loading)
content = replace_once(index, content, '<noscript>',
                       '<noscript><style>#router-mailbox-loading { display: none; }</style>')
for before, after in (
    ('<meta name="theme-color" content="#3367d6">', '<meta name="theme-color" content="#eef3f7">'),
    ('<meta name="color-scheme" content="dark light">', '<meta name="color-scheme" content="light">'),
    ('<meta name="apple-mobile-web-app-title" content="PairDrop">', '<meta name="apple-mobile-web-app-title" content="设备传输站">'),
    ('<meta name="application-name" content="PairDrop">', '<meta name="application-name" content="设备传输站">'),
    ('<meta property="og:title" content="PairDrop">', '<meta property="og:title" content="设备传输站">'),
    ('href="images/favicon-96x96.png"', 'href="images/router-mailbox.svg"'),
    ('<h1>Enable JavaScript</h1>', '<h1>请启用 JavaScript</h1>'),
    ('<h3>PairDrop works only with JavaScript</h3>', '<h3>设备传输站需要 JavaScript 才能运行</h3>'),
):
    # The upstream document has two identical favicon hrefs.
    content = content.replace(before, after)
plans.append((index, content))

manifest = args.release / 'public/manifest.json'
content = json.loads(manifest.read_text(encoding='utf-8'))
content.update(name='设备传输站', short_name='传输站', description='在自己的设备之间传送文字和文件，支持离线暂存。',
               theme_color='#eef3f7', background_color='#eef3f7', lang='zh-CN')
content.pop('screenshots', None)  # Upstream screenshots show the retired radar UI.
plans.append((manifest, json.dumps(content, ensure_ascii=False, indent=4) + '\n'))

main = args.release / 'public/scripts/main.js'
content = main.read_text(encoding='utf-8')
if 'await this.loadAndApplyScript("scripts/router-mailbox.js");' not in content:
    content = replace_once(main, content, '        await this.hydrate();',
                           '        await this.loadAndApplyScript("scripts/router-compat.js");\n        await this.hydrate();')
    content = replace_once(main, content,
                           '        await this.loadAndApplyScript("scripts/router-compat.js");\n        await this.hydrate();',
                           '        await this.loadAndApplyScript("scripts/router-compat.js");\n'
                           '        await this.loadAndApplyScript("scripts/router-mailbox.js");\n        await this.hydrate();')
plans.append((main, content))

ws_server = args.release / 'server/ws-server.js'
content = replace_once(ws_server, ws_server.read_text(encoding='utf-8'),
                       'import {hasher, randomizer} from "./helper.js";',
                       'import {hasher, randomizer} from "./helper.js";\nimport {handleRtcFallback} from "./router-compat.js";')
content = replace_once(ws_server, content, "            case 'join-ip-room':",
                       "            case 'rtc-fallback':\n                handleRtcFallback(this, sender, message);\n                break;\n            case 'join-ip-room':")
content = replace_once(ws_server, content,
                       "        peer.socket.on('message', message => this._onMessage(peer, message));",
                       "        peer.socket.on('message', message => this._onMessage(peer, message));\n"
                       "        // Browser networking answers protocol pings even when page JS is paused.\n"
                       "        peer.socket.on('pong', () => this._setKeepAliveTimerToNow(peer));")
content = replace_once(ws_server, content,
                       "        this._send(peer, { type: 'ping' });",
                       "        if (peer.socket.readyState !== peer.socket.OPEN) {\n"
                       "            this._disconnect(peer);\n"
                       "            return;\n"
                       "        }\n"
                       "        peer.socket.ping();")
# Retain parsing of old JSON pongs for compatibility, but only protocol pongs
# can now refresh liveness. The server no longer sends application-level pings.
content = replace_once(ws_server, content,
                       "            case 'pong':\n                this._setKeepAliveTimerToNow(sender);\n                break;",
                       "            case 'pong':\n                // Legacy application pong; protocol pong updates liveness.\n                break;")
content = replace_once(ws_server, content, '        this._conf = conf',
                       '        this._conf = conf;\n        this._mailbox = server.routerMailbox;')
content = replace_once(ws_server, content,
                       "        peer.socket.on('message', message => this._onMessage(peer, message));",
                       "        this._mailbox?.attachSocket(peer);\n"
                       "        peer.socket.on('message', message => this._onMessage(peer, message));")
plans.append((ws_server, content))

http_server = args.release / 'server/server.js'
content = replace_once(http_server, http_server.read_text(encoding='utf-8'), 'import express from "express";',
                       'import express from "express";\nimport {mountMailbox} from "./router-mailbox.js";')
if '        const mailbox = mountMailbox(app);' not in content:
    if '        mountMailbox(app);' in content:
        content = replace_once(http_server, content, '        mountMailbox(app);',
                               '        const mailbox = mountMailbox(app);')
    else:
        content = replace_once(http_server, content, '        app.use(express.static(publicPathAbs));',
                               '        app.use(express.static(publicPathAbs));\n        const mailbox = mountMailbox(app);')
content = replace_once(http_server, content, '        const server = http.createServer(app);',
                       '        const server = http.createServer(app);\n        server.routerMailbox = mailbox;')
plans.append((http_server, content))

sources = Path(__file__).resolve().parent
plans.extend(((args.release / 'public/scripts/router-compat.js', (sources / 'router-compat-client.js').read_text(encoding='utf-8')),
              (args.release / 'server/router-compat.js', (sources / 'router-compat-server.js').read_text(encoding='utf-8')),
              (args.release / 'public/scripts/router-mailbox.js', (sources / 'router-mailbox-client.js').read_text(encoding='utf-8')),
              (args.release / 'public/styles/router-mailbox.css', (sources / 'router-mailbox.css').read_text(encoding='utf-8')),
              (args.release / 'public/images/router-mailbox.svg', (sources / 'router-mailbox.svg').read_text(encoding='utf-8')),
              (args.release / 'server/router-mailbox.js', (sources / 'router-mailbox-server.js').read_text(encoding='utf-8'))))
plans = [(path, content) for path, content in plans if not path.exists() or path.read_text(encoding='utf-8') != content]

if not args.check:
    for path, content in plans:
        temporary = path.with_name(path.name + '.op7-new')
        temporary.write_text(content, encoding='utf-8', newline='\n')
        temporary.chmod(stat.S_IMODE(path.stat().st_mode) if path.exists() else 0o600)
        temporary.replace(path)
    (args.release / 'LOCAL_OVERLAY').write_text('op7-mailbox5-text1-initial-shell1-native-ping1\n', encoding='utf-8')
print(f'PAIRDROP_WS_MODE_{"CHECKED" if args.check else "APPLIED"} changed_files={len(plans)}')
