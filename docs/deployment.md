# Deployment, backup and upgrade

The binary embeds only distributable templates and assets. `init` exports them
and refuses to overwrite existing files. It does not create a fixed Key.
`serve` loads a snapshot and locks the SQLite file against a second server.

Windows, Linux and macOS use the same CLI. Default listen address is loopback.
For LAN/reverse-proxy deployment, explicitly choose `--listen 0.0.0.0:8787` and
configure your proxy to preserve unbuffered SSE. Body/header/output sizes and
write/shutdown deadlines are bounded.

## Containers

`Dockerfile` builds a pure-Go binary and a scratch image with CA certificates.
Configuration is mounted read-only; a named `/data` volume inherits UID 65532.
Initialize the configuration with `init --shared-config` before `compose up`
so UID 65532 can read the exported configuration and assets. Keep the mounted
configuration directory searchable by that user. Shared files are readable
but not writable by other users; raw credentials and the database are not
created during initialization. Imported assets inherit the manifest's read
permissions, and atomic manifest replacement preserves those permissions.
Create a Key with
`docker compose run --rm --no-deps fakeapi keys create ...`.

For host mock servers, container 127.0.0.1 means the container itself. Use
Docker Desktop's host address or a Linux host-gateway override; another service
in the same Compose network is the portable option.

Logs default to stdout. File logs have bounded rotation. No request body or Key
may be added to proxy/HTTP debug logging by this service.

## Upgrade

1. Read CHANGELOG and protocol/configuration changes.
2. Run `fakeapi db backup --config FILE --out BACKUP` for an online consistent
   SQLite `VACUUM INTO` backup. Do not copy a live SQLite file by itself.
3. Preserve the old binary and configuration, then stop the service.
4. Run the new binary's `config check`, and `db migrate` if required.
5. Start and check health, models and an artificial generation request.

Current database schema is 1, created automatically for a new database. A newer
schema is rejected. Roll back using the previous binary/configuration and the
pre-migration backup. Keep the data volume during Compose upgrades.
Restoring a backup restores its snapshot counters; calls accepted after the backup
are not retained. To recover, stop the service and use a fresh database path/volume
with that backup. Do not overwrite an open database or leave its old WAL/SHM beside
a restored file. `--state PATH` selects the recovered database explicitly.

`scripts/upgrade-smoke.py` compares an immutable previous binary (`b94edf5`)
with the candidate on temporary data. It exercises real preset/AI calls, online
backup, policy/expiry/hash/usage/revocation preservation, same-schema downgrade,
the RPM window, restoration to the backup snapshot and logical non-modification
when a future schema is rejected. CI repeats this on Linux, Windows and macOS.
Current schema remains 1; no schema-2 migration has been introduced or certified.

Release archives are built by `python scripts/build-release.py`. A pushed `v*`
tag first runs the full reusable runtime qualification, then contract/SDK checks
and archive runtime checks before publishing archives and a multi-architecture
GHCR image with repository token permissions. Workflow dispatch only builds.
No GitHub repository or published release is created by local development.

For a published image, use the archive's `compose.yaml` with an explicit image tag,
e.g. `FAKEAPI_IMAGE=ghcr.io/cyrusauyeung/fakeapi:v0.5.0-alpha.2`.
Initialize `runtime/` using that image's `init --shared-config`, create a Key,
and run `docker compose up --no-build -d`. Source `docker compose build` requires
the source checkout; portable archives contain the executable and deployment configuration.
