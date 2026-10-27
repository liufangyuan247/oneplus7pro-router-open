"""Initialize a new, loopback-only AdGuard Home instance without logging secrets."""
import base64
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.request

home = Path(os.environ["HOME"])
prefix = Path(os.environ["PREFIX"])
data = home / "router-data/adguardhome"
config = data / "AdGuardHome.yaml"
initialized = config.exists()
if initialized and os.environ.get("ADGUARD_RESUME") != "1":
    raise SystemExit("Refusing to overwrite existing AdGuard Home configuration")
credentials = json.loads((home / ".stage/adguardhome/credentials.json").read_text())
auth = base64.b64encode((credentials["username"] + ":" + credentials["password"]).encode()).decode()
env = dict(os.environ, LD_PRELOAD="", SSL_CERT_FILE=str(prefix / "etc/tls/cert.pem"))
data.mkdir(parents=True, exist_ok=True)
os.chmod(data, 0o700)


def api(path, payload=None, method=None, authenticated=True):
    body = None if payload is None else json.dumps(payload).encode()
    headers = {"Content-Type": "application/json"}
    if authenticated:
        headers["Authorization"] = "Basic " + auth
    req = urllib.request.Request("http://127.0.0.1:13001/control/" + path,
                                 data=body, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=90) as response:
        result = response.read()
        return json.loads(result) if result.lstrip().startswith((b"{", b"[")) else None


with (data / "bootstrap.log").open("wb") as log:
    # Upstream requires root for its initial permission check even when the
    # final DNS port is unprivileged. Only initialization runs as root.
    pidfile = data / "bootstrap.pid"
    command = (f'umask 077; echo $$ > {pidfile}; '
               f'chown {os.getuid()}:{os.getgid()} {pidfile}; '
               f'exec {prefix}/bin/AdGuardHome -c {config} -w {data} '
               '--web-addr 127.0.0.1:13001 --no-check-update')
    proc = subprocess.Popen([str(prefix / "bin/su"), "-c", command],
                            env=env, stdout=log, stderr=log)
    try:
        for attempt in range(30):
            try:
                api("status" if initialized else "install/get_addresses", authenticated=initialized)
                break
            except Exception:
                if proc.poll() is not None or attempt == 29:
                    raise RuntimeError("AdGuard Home bootstrap did not start") from None
                time.sleep(1)
        if not initialized:
            api("install/configure", {
                **credentials,
                "web": {"ip": "0.0.0.0", "port": 3001},
                "dns": {"ip": "0.0.0.0", "port": 5354},
                "language": "zh-cn",
            }, authenticated=False)
        api("dns_config", {
            "upstream_dns": ["127.0.0.1:53"], "fallback_dns": [],
            "bootstrap_dns": ["127.0.0.1:53"], "local_ptr_upstreams": ["127.0.0.1:53"],
            "protection_enabled": True, "blocking_mode": "null_ip",
            "blocked_response_ttl": 60, "ratelimit": 0,
            "cache_enabled": False, "use_private_ptr_resolvers": True,
            "resolve_clients": False,
        })
        api("filtering/set_rules", {"rules": [
            "||adguard-health.invalid^$dnsrewrite=NOERROR;A;127.0.0.1"
        ]})
        api("filtering/config", {"enabled": True, "interval": 24})
        # Keep bounded local diagnostics; health checks are excluded.
        api("querylog/config/update", {"enabled": True, "interval": 86400000,
            "anonymize_client_ip": False, "ignored": ["adguard-health.invalid"],
            "ignored_enabled": True}, method="PUT")
        api("stats/config/update", {"enabled": True, "interval": 604800000,
            "ignored": ["adguard-health.invalid"], "ignored_enabled": True}, method="PUT")
        print("ADGUARD_CONFIGURED upstream=existing-dnsmasq logs=1day stats=7days")
    finally:
        if pidfile.exists():
            pid = int(pidfile.read_text().strip())
            subprocess.run([str(prefix / "bin/su"), "-c", f"kill -TERM {pid}"],
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            proc.wait(timeout=15)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
        subprocess.run([str(prefix / "bin/su"), "-c",
                        f"chown -R {os.getuid()}:{os.getgid()} {data}"], check=True)
os.chmod(config, 0o600)
