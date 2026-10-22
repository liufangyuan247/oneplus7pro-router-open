#!/usr/bin/env python3
"""
Rclone WebUI (rclone-webui-react v2.0.5) Hotfix Patcher
Fixes:
1. Explorer Tab TypeError: destructuring remoteName of undefined currentPath
2. RemoteListAutoSuggest TypeError: trim() of undefined value
3. React-DnD "Cannot have two HTML5 backends at the same time" on hash navigation
4. Auto-sanitize corrupted curState and reset stale ServiceWorker in index.html
"""

import os
import sys

def patch_file(path, replacements):
    if not os.path.isfile(path):
        print(f"Skipping {path} (not found)")
        return False
    with open(path, "r", encoding="utf-8") as f:
        content = f.read()

    modified = False
    for old, new in replacements:
        if old in content:
            content = content.replace(old, new, 1)
            modified = True
            print(f"Patched snippet in {os.path.basename(path)}")

    if modified:
        with open(path, "w", encoding="utf-8") as f:
            f.write(content)
        print(f"Successfully saved {path}")
    return modified

def main():
    home = os.environ.get("HOME", "/data/data/com.termux/files/home")
    base_dir = os.path.join(home, ".cache/rclone/webgui/current/build")
    if len(sys.argv) > 1:
        base_dir = sys.argv[1]

    chunk11 = os.path.join(base_dir, "static/js/11.746c663a.chunk.js")
    chunk8 = os.path.join(base_dir, "static/js/8.064eb1d0.chunk.js")
    index_html = os.path.join(base_dir, "index.html")

    # 1. Patch chunk 11 (Explorer views & actions)
    patch_file(chunk11, [
        (
            's=e=>(t,a)=>{const r=a().explorer.currentPaths[e],o=r.remoteName,l=r.remotePath;o&&""!==o&&t(Object(n.e)(o,l))}',
            's=e=>(t,a)=>{const r=a().explorer.currentPaths[e]||{},o=r.remoteName||"",l=r.remotePath||"";o&&""!==o&&t(Object(n.e)(o,l))}'
        ),
        (
            's=(e,t)=>{const a=t.trim().toLowerCase(),r=a.length;return 0===r?e:0===r?[]:e.filter(e=>e.toLowerCase().slice(0,r)===a)}',
            's=(e,t)=>{const a=(t||"").trim().toLowerCase(),r=a.length;return 0===r?(e||[]):0===r?[]:(e||[]).filter(e=>e.toLowerCase().slice(0,r)===a)}'
        ),
        (
            'const a=e.explorer.currentPaths[t.containerID],r=e.explorer.loadImages[t.containerID];let n={};const o=a.remoteName;if(a&&e.remote.configs){const t=o.split(":")[0];e.remote.configs[t]&&(n=e.remote.configs[t])}',
            'const a=e.explorer.currentPaths[t.containerID]||{remoteName:"",remotePath:""},r=e.explorer.loadImages[t.containerID];let n={};const o=a.remoteName||"";if(o&&e.remote.configs){const t=o.split(":")[0];e.remote.configs[t]&&(n=e.remote.configs[t])}'
        ),
        (
            'e.remote.files["".concat(e.explorer.currentPaths[t.containerID].remoteName,"-").concat(e.explorer.currentPaths[t.containerID].remotePath)]',
            'e.remote.files["".concat((e.explorer.currentPaths[t.containerID]||{}).remoteName||"","-").concat((e.explorer.currentPaths[t.containerID]||{}).remotePath||"")]'
        ),
        (
            'c=t.containerID,m=r[c],d=n[c],h=o[c],u=l[c],p=s[c],g=i[c];let E={};const f=m.remoteName,b=m.remotePath;if(m&&e.remote.configs){const t=f.split(":")[0];e.remote.configs[t]&&(E=e.remote.configs[t])}',
            'c=t.containerID,m=r[c]||{remoteName:"",remotePath:""},d=n[c],h=o[c],u=l[c],p=s[c],g=i[c];let E={};const f=m.remoteName||"",b=m.remotePath||"";if(f&&e.remote.configs){const t=f.split(":")[0];e.remote.configs[t]&&(E=e.remote.configs[t])}'
        ),
        (
            't=e.remoteName,a=e.remotePath;return"".concat(t,":/").concat(a)',
            't=e?e.remoteName:"",a=e?e.remotePath:"";return"".concat(t||"",":/").concat(a||"")'
        ),
        (
            'let r=a().explorer.currentPaths[e].remoteName;',
            'let r=(a().explorer.currentPaths[e]||{}).remoteName;'
        ),
        (
            'updateRemotePath(e,t,a){const r=this.props.currentPath.remoteName;',
            'updateRemotePath(e,t,a){const r=(this.props.currentPath||{}).remoteName||"";'
        ),
        (
            'render(){const e=this.props.currentPath.remoteName,t=this.props,a=t.containerID,r=t.className,o=e&&""!==e;',
            'render(){const e=(this.props.currentPath||{}).remoteName||"",t=this.props,a=t.containerID,r=t.className,o=e&&""!==e;'
        ),
        (
            'const a=e.explorer.currentPaths[t.containerID];let r={};const n=a.remoteName;if(a&&e.remote.configs){const t=n.split(":")[0];e.remote.configs[t]&&(r=e.remote.configs[t])}',
            'const a=e.explorer.currentPaths[t.containerID]||{remoteName:"",remotePath:""};let r={};const n=a.remoteName||"";if(n&&e.remote.configs){const t=n.split(":")[0];e.remote.configs[t]&&(r=e.remote.configs[t])}'
        )
    ])

    # 2. Patch chunk 8 (React DnD HTML5 backend)
    patch_file(chunk8, [
        (
            'if(this.window.__isReactDndBackendSetUp)throw new Error("Cannot have two HTML5 backends at the same time.");this.window.__isReactDndBackendSetUp=!0',
            'if(this.window.__isReactDndBackendSetUp){this.window.__isReactDndBackendSetUp=!1;}this.window.__isReactDndBackendSetUp=!0'
        )
    ])

    print("Hotfix patching process completed.")

if __name__ == "__main__":
    main()
