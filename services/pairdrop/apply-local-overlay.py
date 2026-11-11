"""Add automatic, per-pair WebRTC fallback to the pinned PairDrop release."""
import argparse
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
new_version = "const cacheVersion = 'v1.11.2-op7-mailbox4';"
plans = []


def replace_once(path, content, old, new):
    if new in content:
        return content
    if content.count(old) != 1:
        raise SystemExit(f'Unsupported PairDrop source; refusing to patch {path.name}')
    return content.replace(old, new, 1)


plans.append((util, replace_once(util, util.read_text(encoding='utf-8'), original, replacement)))
content = worker.read_text(encoding='utf-8').replace(previous_version, old_version).replace(previous_auto_version, old_version).replace(previous_mailbox_version, old_version).replace(previous_ui_version, old_version).replace(previous_presence_version, old_version)
content = replace_once(worker, content, old_version, new_version)
content = replace_once(worker, content, "    'scripts/main.js',", "    'scripts/router-compat.js',\n    'scripts/main.js',")
content = replace_once(worker, content, "    'scripts/router-compat.js',",
                       "    'scripts/router-mailbox.js',\n    'styles/router-mailbox.css',\n    'scripts/router-compat.js',")
content = replace_once(worker, content,
                       "self.addEventListener('fetch', function(event) {",
                       "self.addEventListener('fetch', function(event) {\n"
                       "    // File-box requests are ordinary streaming HTTP, not Web Share Target posts.\n"
                       "    if (new URL(event.request.url).pathname.startsWith('/mailbox/api/')) {\n"
                       "        event.respondWith(fetch(event.request));\n"
                       "        return;\n"
                       "    }")
plans.append((worker, content))

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
              (args.release / 'server/router-mailbox.js', (sources / 'router-mailbox-server.js').read_text(encoding='utf-8'))))
plans = [(path, content) for path, content in plans if not path.exists() or path.read_text(encoding='utf-8') != content]

if not args.check:
    for path, content in plans:
        temporary = path.with_name(path.name + '.op7-new')
        temporary.write_text(content, encoding='utf-8', newline='\n')
        temporary.chmod(stat.S_IMODE(path.stat().st_mode) if path.exists() else 0o600)
        temporary.replace(path)
    (args.release / 'LOCAL_OVERLAY').write_text('op7-mailbox4-text1-native-ping1\n', encoding='utf-8')
print(f'PAIRDROP_WS_MODE_{"CHECKED" if args.check else "APPLIED"} changed_files={len(plans)}')
