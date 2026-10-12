# compude-test

Minimal workload for testing Compulyst deployments.

- Listens on `:8080`.
- `GET /healthz` — 200 when healthy (Docker `HEALTHCHECK` uses it).
- `GET /` — JSON with version, hostname, and whether `APP_SECRET` / `GREETING` are set.
- `GET /slow?ms=5000` — long request, for drain testing.
- `POST /_platform/drain` — stop accepting new requests, wait for in-flight ones, return 200.

Release behaviour comes from `release.json`:

```json
{"version": "v1", "healthy": true, "crash_after_seconds": 0}
```

Set `healthy` to `false` or `crash_after_seconds` above 0 to produce a broken release.
