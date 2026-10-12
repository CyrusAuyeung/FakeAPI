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
Initialize the configuration before `compose up` and create a Key with
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

Release archives are built by `python scripts/build-release.py`. A pushed `v*`
tag runs contract/SDK checks, then publishes archives and a multi-architecture
GHCR image with repository token permissions. Workflow dispatch only builds.
No GitHub repository or published release is created by local development.
