# Batch and automatic Gemini and Claude aliases

`sync-model-aliases.py` reads the local CPA `/v1/models` catalog and adds all
unambiguous Antigravity Gemini and Claude aliases in one management API PATCH.
It uses the existing client and management key files on the phone; callers do
not supply credentials or upstream URLs. No CPA source patch is required.

For example, `gemini-4-neon-xhigh` gains the additional client name
`gemini-4-neon`, and `gemini-3.8-flash-high` gains `gemini-3.8-flash`. Every
generated entry uses `fork: true`, keeping the original model ID available.
Response-model rewriting is not enabled.

For Claude, `claude-sonnet-5-5-high` gains `claude-sonnet-5-5` and the same-version
spelling `claude-sonnet-5.5`; Opus and Haiku follow the same version rule. Both
spellings are checked for native-name and manual-alias conflicts independently.
Claude credentials outside Antigravity are not changed. Unversioned names such
as `claude-sonnet` are not generated as implicit latest-model selectors.

Only terminal `-xhigh`, `-high`, `-medium`, and `-low` variants are considered.
An existing real model keeps its name. Existing manual aliases, their options,
and other providers are preserved. If multiple variants would produce the same
alias, the helper reports the candidates and skips it. Existing aliases are
never automatically retargeted or pruned, including during quota cooldowns.
Preview/stable names, different versions, product codenames, image variants and
`latest` aliases are not inferred across model families. A model absent from the
account's registered catalog cannot be enabled by inventing an alias.

## Install on the existing phone

Stage `sync-model-aliases.py`, `model-alias-sync-hook.sh`, and
`install-model-alias-sync.py` under Termux `~/.stage/cpa-model-aliases/` with LF
line endings. Independently record SHA256 for the first two files, then run:

```sh
python ~/.stage/cpa-model-aliases/install-model-alias-sync.py <helper-sha256> <hook-sha256>
```

The installer verifies hashes and shell/Python syntax, backs up the previous
helper, policy and health monitor, applies the initial batch, and restarts only
`healthd` to attach the scheduling hook. CPA and other services stay running.
For fresh router installs, `install-termux.sh` also sources the hook when it is
present. The existing health monitor starts the short-lived helper at most once
every five minutes; the helper itself contacts CPA only when due. No additional
resident daemon is installed.

The initial policy enables a local catalog check every 15 minutes. CPA still
controls its upstream catalog/entitlement refresh cadence (currently three
hours). New matching models gain aliases on a subsequent local check. Scheduled
failures back off for five minutes and retain the last working configuration.
Every changing batch saves the previous provider mapping, checks for concurrent
edits, confirms model-list registration, and checks that the rest of CPA's
configuration is unchanged. Failed confirmation restores the previous provider
mapping only when nobody has edited it afterward. Ambiguous network failures
are reconciled by reading CPA before retrying a write.

## Use

```sh
# Preview all proposed aliases without changing CPA.
python "$PREFIX/bin/cpa-model-alias-sync"

# Apply the entire batch once; safe to repeat.
python "$PREFIX/bin/cpa-model-alias-sync" --apply

# Read policy, last outcome, skipped candidates and next scheduled check.
python "$PREFIX/bin/cpa-model-alias-sync" --status
```

Private runtime files under `~/router-data/cpa/`:

- `model-alias-sync-policy.json`: `{"enabled":true,"intervalSeconds":900}`.
  Set `enabled` to `false` to stop future automatic batches; aliases already
  added remain usable. Accepted check intervals are 300–86400 seconds.
- `model-alias-sync-status.json`: last outcome, proposed/added aliases, conflicts
  and next check timestamp. Runtime failures omit HTTP bodies and credentials.
- `model-alias-sync.lock`: kernel lock shared by manual and scheduled runs.
- `model-alias-backups/`: previous Antigravity mappings for changing batches.

Policy, state and mapping snapshots are private files and must not be committed.
Manual `--apply` remains available even when automatic scheduling is disabled.

Run `python test_sync_model_aliases.py` on Termux for the planning, concurrent
edit, rollback, idempotency, policy scheduling and real file-lock tests. The
tests use fake API responses and temporary paths and do not contact production.
