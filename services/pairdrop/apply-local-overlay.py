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
new_version = "const cacheVersion = 'v1.11.2-op7-auto-ws2';"
plans = []


def replace_once(path, content, old, new):
    if new in content:
        return content
    if content.count(old) != 1:
        raise SystemExit(f'Unsupported PairDrop source; refusing to patch {path.name}')
    return content.replace(old, new, 1)


plans.append((util, replace_once(util, util.read_text(encoding='utf-8'), original, replacement)))
content = worker.read_text(encoding='utf-8').replace(previous_version, old_version)
content = replace_once(worker, content, old_version, new_version)
content = replace_once(worker, content, "    'scripts/main.js',", "    'scripts/router-compat.js',\n    'scripts/main.js',")
plans.append((worker, content))

main = args.release / 'public/scripts/main.js'
content = replace_once(main, main.read_text(encoding='utf-8'), '        await this.hydrate();',
                       '        await this.loadAndApplyScript("scripts/router-compat.js");\n        await this.hydrate();')
plans.append((main, content))

ws_server = args.release / 'server/ws-server.js'
content = replace_once(ws_server, ws_server.read_text(encoding='utf-8'),
                       'import {hasher, randomizer} from "./helper.js";',
                       'import {hasher, randomizer} from "./helper.js";\nimport {handleRtcFallback} from "./router-compat.js";')
content = replace_once(ws_server, content, "            case 'join-ip-room':",
                       "            case 'rtc-fallback':\n                handleRtcFallback(this, sender, message);\n                break;\n            case 'join-ip-room':")
plans.append((ws_server, content))

sources = Path(__file__).resolve().parent
plans.extend(((args.release / 'public/scripts/router-compat.js', (sources / 'router-compat-client.js').read_text(encoding='utf-8')),
              (args.release / 'server/router-compat.js', (sources / 'router-compat-server.js').read_text(encoding='utf-8'))))
plans = [(path, content) for path, content in plans if not path.exists() or path.read_text(encoding='utf-8') != content]

if not args.check:
    for path, content in plans:
        temporary = path.with_name(path.name + '.op7-new')
        temporary.write_text(content, encoding='utf-8', newline='\n')
        temporary.chmod(stat.S_IMODE(path.stat().st_mode) if path.exists() else 0o600)
        temporary.replace(path)
    (args.release / 'LOCAL_OVERLAY').write_text('op7-auto-ws2\n', encoding='utf-8')
print(f'PAIRDROP_WS_MODE_{"CHECKED" if args.check else "APPLIED"} changed_files={len(plans)}')
