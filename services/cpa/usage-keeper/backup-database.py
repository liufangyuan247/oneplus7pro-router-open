"""Create and verify a standalone SQLite backup after Keeper has stopped."""
import hashlib
import json
from pathlib import Path
import sqlite3
import sys

source, destination = map(Path, sys.argv[1:])
if destination.exists():
    raise SystemExit(f"Refusing to overwrite backup: {destination}")
with sqlite3.connect(source.resolve().as_uri() + "?mode=ro", uri=True) as src:
    with sqlite3.connect(destination) as dst:
        src.backup(dst)
with sqlite3.connect(destination.resolve().as_uri() + "?mode=ro", uri=True) as db:
    checks = [row[0] for row in db.execute("PRAGMA integrity_check")]
    if checks != ["ok"]:
        raise SystemExit(f"Backup integrity check failed: {checks}")
    tables = {row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
    counts = {}
    for table in ("usage_events", "usage_raw_messages", "usage_request_logs"):
        if table in tables:
            counts[table] = db.execute(f'SELECT COUNT(*) FROM "{table}"').fetchone()[0]
with destination.open("rb") as backup:
    digest = hashlib.file_digest(backup, "sha256").hexdigest()
manifest = {"database": destination.name, "sha256": digest, "bytes": destination.stat().st_size,
            "integrityCheck": "ok", "rowCounts": counts}
destination.with_suffix(".backup.json").write_text(json.dumps(manifest, indent=2) + "\n")
print(json.dumps(manifest))
