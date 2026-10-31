"""Activate the prepared native Termux release under runit (first install)."""
import argparse
import ipaddress
import os
from pathlib import Path
import secrets
import shlex
import shutil
import subprocess
import time
import urllib.request


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--bind', default='127.0.0.1')
    args = parser.parse_args()
    host = str(ipaddress.IPv4Address(args.bind))
    home = Path.home()
    prefix = Path(os.environ['PREFIX'])
    stage = home / '.stage/searxng-install'
    app = home / 'router-apps/searxng'
    release = app / 'releases/ffa88e3ba3ade64185d5754c839cdf5a80708c4a'
    data = home / 'router-data/searxng'
    service = home / 'router-stack/services/searxng'
    assert not service.exists(), 'Service already exists; use a separate upgrade transaction'
    for name in ('router_ddg_get.py', 'router_auto.py', 'router_tavily.py'):
        shutil.copyfile(stage / name, release / 'searx/engines' / name)
    shutil.copyfile(stage / 'router_search.py', app / 'router_search.py')
    # Same generated metadata format used by upstream's source-archive builds.
    version = '2026.10.30+ffa88e3ba3ad'
    (release / 'UPSTREAM_VERSION').write_text('2026.10.30\n')
    (release / 'searx/version_frozen.py').write_text(
        'VERSION_STRING = ' + repr(version) + '\nVERSION_TAG = VERSION_STRING\n'
        + "DOCKER_TAG = '2026.10.30-ffa88e3ba3ad'\n"
        + "GIT_URL = 'https://github.com/searxng/searxng'\nGIT_BRANCH = 'master'\n")
    subprocess.run([str(release / 'venv/bin/python'), '-c',
                    'import lxml.etree, yaml, msgspec, curl_cffi, waitress'], check=True)
    data.mkdir(parents=True, exist_ok=True, mode=0o700)
    (data / 'cache').mkdir(exist_ok=True, mode=0o700)
    config = data / 'settings.yml'
    if not config.exists():
        config.write_text((stage / 'settings.example.yml').read_text().replace('CHANGE_ME', secrets.token_hex(32)))
        config.chmod(0o600)
    (data / 'listen.env').write_text('export SEARXNG_BIND_ADDRESS=' + shlex.quote(host) + '\n')
    (data / 'listen.env').chmod(0o600)
    shutil.copyfile(stage / 'serve.py', app / 'serve.py')
    (app / 'current.new').symlink_to(release)
    (app / 'current.new').replace(app / 'current')
    candidate = stage / ('service.' + str(int(time.time())))
    logdir = candidate / 'log/main'
    logdir.mkdir(parents=True, mode=0o700)
    shutil.copyfile(stage / 'searxng-service-run', candidate / 'run')
    (candidate / 'run').chmod(0o700)
    (candidate / 'log/run').write_text(
        '#!' + str(prefix / 'bin/sh') + '\nexec ' + shlex.quote(str(prefix / 'bin/svlogd'))
        + ' -tt ' + shlex.quote(str(service / 'log/main')) + '\n')
    (candidate / 'log/run').chmod(0o700)
    (logdir / 'config').write_text('s262144\nn2\n')
    (candidate / 'down').touch(mode=0o600)
    candidate.rename(service)
    (service / 'down').unlink()
    try:
        subprocess.run(['sv', 'up', str(service)], capture_output=True, timeout=10)
        client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        for attempt in range(45):
            try:
                with client.open('http://' + host + ':8888/healthz', timeout=2) as response:
                    assert response.status == 200
                print('SEARXNG_ACTIVATED')
                return
            except Exception:
                if attempt == 44:
                    raise
                time.sleep(1)
    except Exception:
        (service / 'down').touch(mode=0o600)
        subprocess.run(['sv', 'down', str(service)], capture_output=True, timeout=10)
        raise


if __name__ == '__main__':
    main()
