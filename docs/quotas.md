# Key and quota semantics

Keys contain a random public ID and 32-byte random secret. SQLite stores a
SHA-256 digest of the full high-entropy Key; verification is constant-time.
No plaintext recovery is available.

Policies are copied into a Key at creation or explicit update. YAML edits do
not automatically mutate existing Keys. Total/AI/RPM limits are nullable;
null means unlimited, zero means disallowed. Concurrency must be positive.

The service acquires in-memory service/Key/provider capacity before an SQLite
`BEGIN IMMEDIATE` transaction. It rechecks revocation, expiry, current limits,
RPM, and concurrency against current metadata. Counters and the minute window
are updated atomically. AI quota failure does not debit total quota.

| Request | Total | AI | RPM |
|---|---:|---:|---:|
| Invalid input, auth, model, capability or constraints | 0 | 0 | 0 |
| Capacity/rate/quota rejection | 0 | 0 | 0 |
| Admitted preset, including failure/cancel | +1 | 0 | +1 |
| Admitted AI attempt, including backup/failure/cancel | +1 | +1 | +1 |
| Model catalogs, health, local CLI | 0 | 0 | 0 |

There are no refunds or automatic billing-period resets. Each client retry is
a new admission. Counters measure local admitted attempts, not upstream bills.
Expiry uses UTC and rejects `now >= expires_at`. Existing admitted streams
continue after Key revocation or expiry.

RPM uses a fixed UTC minute window, persists across restart and does not reset
when the clock moves backward. Active concurrency exists only in memory; all
other Key data persists. Lowering concurrency allows current streams to finish
while rejecting new requests until below the new limit.

One server owns a database-side file lock. Local CLI management uses independent
SQLite connections. WAL/FULL/foreign_keys and a bounded busy timeout are enabled.
Use local disk and one server instance per database.
