#!/data/data/com.termux/files/usr/bin/python
"""Install the batch helper and its optional healthd scheduling hook."""
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

HOME_DIR = Path('/data/data/com.termux/files/home')
PREFIX = Path('/data/data/com.termux/files/usr')
STAGE = HOME_DIR / '.stage/cpa-model-aliases'
DATA = HOME_DIR / 'router-data/cpa'
HEALTH = HOME_DIR / 'router-stack/bin/healthd'
HOOK = HEALTH.with_name('cpa-model-alias-sync-hook')
HELPER = PREFIX / 'bin/cpa-model-alias-sync'
POLICY = DATA / 'model-alias-sync-policy.json'
SERVICE = HOME_DIR / 'router-stack/services/healthd'
MARKER = '# CPA_MODEL_ALIAS_SYNC'


def atomic(path, content, mode):
    temp = path.with_name(path.name + '.alias-install-new')
    with temp.open('wb') as stream:
        os.chmod(temp, mode)
        stream.write(content)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temp, path)


def main():
    os.umask(0o077)
    if os.geteuid() != (SERVICE / 'run').stat().st_uid:
        raise RuntimeError('Run as the Termux service user')
    if len(sys.argv) != 3:
        raise RuntimeError('Expected helper and hook SHA256 digests')
    helper = (STAGE / 'sync-model-aliases.py').read_bytes()
    hook = (STAGE / 'model-alias-sync-hook.sh').read_bytes()
    if [hashlib.sha256(x).hexdigest() for x in [helper, hook]] != sys.argv[1:]:
        raise RuntimeError('Staging checksum mismatch')
    compile(helper, 'sync-model-aliases.py', 'exec')
    subprocess.run([str(PREFIX / 'bin/sh'), '-n', str(STAGE / 'model-alias-sync-hook.sh')], check=True)
    health = HEALTH.read_text()
    if MARKER not in health:
        anchor = '  sleep 60\n'
        if health.count(anchor) != 1:
            raise RuntimeError('Unexpected healthd layout; no files changed')
        block = ('  ' + MARKER + '\n'
                 '  if [ -r "' + str(HOOK) + '" ]; then\n'
                 '    . "' + str(HOOK) + '"\n'
                 '  fi\n')
        health = health.replace(anchor, block + anchor, 1)
    candidate = STAGE / 'healthd.candidate'
    candidate.write_text(health)
    subprocess.run([str(PREFIX / 'bin/sh'), '-n', str(candidate)], check=True)
    backup = HOME_DIR / 'router-data/backups/cpa' / ('model-alias-sync-' + dt.datetime.now().strftime('%Y%m%d-%H%M%S'))
    backup.mkdir(parents=True, mode=0o700)
    paths = [HEALTH, HELPER, HOOK, POLICY]
    previous = {}
    for index, path in enumerate(paths):
        previous[path] = path.exists()
        if path.exists():
            shutil.copy2(path, backup / str(index))
    was_running = subprocess.run([str(PREFIX/'bin/sv'),'status',str(SERVICE)],capture_output=True).stdout.startswith(b'run:')
    activated = False
    try:
        activated = True
        atomic(HELPER, helper, 0o700)
        atomic(HOOK, hook, 0o700)
        if not POLICY.exists():
            atomic(POLICY, b'{"enabled":true,"intervalSeconds":900}\n', 0o600)
        atomic(HEALTH, health.encode(), 0o700)
        run = subprocess.run([str(PREFIX/'bin/python'),str(HELPER),'--apply'],capture_output=True,text=True,timeout=120)
        result = json.loads(run.stdout)
        if run.returncode:
            raise RuntimeError('Initial sync failed: ' + str(result.get('error','unknown error')))
        if was_running:
            subprocess.run([str(PREFIX/'bin/sv'),'restart',str(SERVICE)],capture_output=True,check=True,timeout=20)
        print(json.dumps({'installed':True,'batch':result,'backup':str(backup),'healthd_restarted':was_running}))
    except BaseException:
        if activated:
            for index, path in enumerate(paths):
                if previous[path]:
                    atomic(path,(backup/str(index)).read_bytes(),(backup/str(index)).stat().st_mode & 0o777)
                else:
                    path.unlink(missing_ok=True)
            if was_running:
                subprocess.run([str(PREFIX/'bin/sv'),'restart',str(SERVICE)],capture_output=True,timeout=20)
        raise


if __name__ == '__main__':
    main()
