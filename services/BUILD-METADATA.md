# Custom service build metadata

Every service compiled locally from an upstream release should install a JSON
manifest at `~/router-data/<service-name>/build-info.json`.

```json
{
  "schemaVersion": 1,
  "service": "example-service",
  "product": "Example Service",
  "customVersion": "v1.2.3-op7.1",
  "upstreamVersion": "v1.2.3",
  "upstreamCommit": "full-source-commit",
  "upstreamRepository": "owner/repository",
  "variant": "op7.1",
  "buildDate": "2026-10-14T03:37:31Z",
  "platform": "android/arm64",
  "sha256": "lowercase-artifact-sha256",
  "patches": ["local-change-id"]
}
```

`customVersion` is the version printed by the deployed artifact.
`upstreamVersion` is the formal release used as the source base.
`upstreamRepository` is the GitHub `owner/repository` used for latest-release
checks. When this manifest exists, Router Admin displays the build as custom,
compares the latest release against `upstreamVersion`, and disables the generic
one-click binary updater so a platform-specific build is never overwritten by
an incompatible upstream artifact.

Installers must verify `sha256` before activation and deploy the binary and
manifest as one rollback unit.
