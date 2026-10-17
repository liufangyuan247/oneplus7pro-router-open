#!/usr/bin/env python3
"""Repair obsolete OpenWrt web asset settings in a Termux OpenList install."""

import argparse
import base64
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess
import tempfile
import time
import urllib.request


LOGO_SHA256 = "fe09f04c24101c9b4b8baada86cf77913bc50731082b102f61ad6260626b43db"
KEYS = ("logo", "favicon", "audio_cover", "customize_head")
LEGACY_SCRIPT = re.compile(
    r'''<script\b[^>]*\bsrc=["']/assets/polyfill\.min\.js(?:\?[^"']*)?["'][^>]*>\s*</script>\s*''',
    re.I,
)


def connect(path):
    # Never create an empty database if the configured path is incorrect.
    return sqlite3.connect(path.resolve().as_uri() + "?mode=rw", uri=True)


def read_settings(conn):
    tables = []
    for (name,) in conn.execute(
        "SELECT name FROM sqlite_master WHERE type='table' AND name LIKE '%setting%'"
    ):
        quoted = '"' + name.replace('"', '""') + '"'
        columns = {row[1] for row in conn.execute("PRAGMA table_info(" + quoted + ")")}
        if {"key", "value"} <= columns:
            tables.append(quoted)
    if len(tables) != 1:
        raise RuntimeError("Expected exactly one OpenList settings table")
    table = tables[0]
    values = dict(conn.execute(
        "SELECT key,value FROM " + table + " WHERE key IN (?,?,?,?)", KEYS
    ))
    return table, values


def plan_updates(values, logo_uri):
    updates = {
        key: logo_uri for key in KEYS[:3] if values.get(key) == "/assets/logo.svg"
    }
    head = values.get("customize_head") or ""
    new_head = LEGACY_SCRIPT.sub("", head)
    if new_head != head:
        updates["customize_head"] = new_head
    return updates


def verify(base_url, updates):
    last_error = None
    for _ in range(30):
        try:
            with urllib.request.urlopen(base_url + "/api/public/settings", timeout=2) as response:
                body = json.load(response)
            if body.get("code") != 200:
                raise RuntimeError("Public settings API failed")
            for key in KEYS[:3]:
                if key in updates and body["data"].get(key) != updates[key]:
                    raise RuntimeError("Public setting did not update: " + key)
            for route in ("/", "/@manage"):
                with urllib.request.urlopen(base_url + route, timeout=2) as response:
                    html = response.read().decode("utf-8")
                if LEGACY_SCRIPT.search(html):
                    raise RuntimeError("Page still includes the obsolete polyfill")
            return
        except Exception as error:
            last_error = error
            time.sleep(1)
    raise RuntimeError("Post-update web verification failed") from last_error


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--logo", required=True, type=Path, help="Official SVG with the pinned SHA-256")
    parser.add_argument("--home", type=Path, default=Path.home(), help="Termux home directory")
    parser.add_argument("--prefix", type=Path, default=Path(os.environ.get(
        "PREFIX", "/data/data/com.termux/files/usr"
    )))
    parser.add_argument("--base-url", default="http://127.0.0.1:5244")
    parser.add_argument("--check", action="store_true", help="Report affected keys without changing anything")
    args = parser.parse_args()
    svg = args.logo.read_bytes()
    if hashlib.sha256(svg).hexdigest() != LOGO_SHA256:
        raise ValueError("Logo SHA-256 mismatch; database was not changed")
    logo_uri = "data:image/svg+xml;base64," + base64.b64encode(svg).decode("ascii")
    db_path = args.home / "router-data/openlist/data.db"
    with closing(connect(db_path)) as conn:
        _, values = read_settings(conn)
    updates = plan_updates(values, logo_uri)
    if not updates or args.check:
        print(json.dumps({
            "result": "repair_needed" if updates else "no_legacy_web_asset_references",
            "changedKeys": sorted(updates),
        }))
        return

    os.umask(0o077)
    backup_root = args.home / "router-data/backups/openlist"
    backup_root.mkdir(parents=True, exist_ok=True)
    backup_dir = Path(tempfile.mkdtemp(
        prefix="web-assets-" + time.strftime("%Y%m%d-%H%M%S") + "-", dir=backup_root
    ))
    backup_path = backup_dir / "data.db"
    service = str(args.home / "router-stack/services/openlist")
    sv = str(args.prefix / "bin/sv")

    def control(action):
        subprocess.run([sv, "-w", "20", action, service], check=True, timeout=25)

    changed = False
    try:
        control("down")
        with closing(connect(db_path)) as conn:
            # Preserve settings saved between initial inspection and shutdown.
            table, values = read_settings(conn)
            updates = plan_updates(values, logo_uri)
            with closing(sqlite3.connect(backup_path)) as backup:
                conn.backup(backup)
            (backup_dir / "web-settings-before.json").write_text(
                json.dumps(values, ensure_ascii=False), encoding="utf-8"
            )
            changed = True
            with conn:
                for key, value in updates.items():
                    conn.execute("UPDATE " + table + " SET value=? WHERE key=?", (value, key))
        control("up")
        verify(args.base_url.rstrip("/"), updates)
    except BaseException:
        try:
            if changed:
                control("down")
                with closing(connect(backup_path)) as source, closing(connect(db_path)) as target:
                    source.backup(target)
        finally:
            control("up")
        raise
    print(json.dumps({
        "result": "OPENLIST_WEB_ASSETS_REPAIRED",
        "changedKeys": sorted(updates),
        "backup": str(backup_dir),
    }))


if __name__ == "__main__":
    main()
