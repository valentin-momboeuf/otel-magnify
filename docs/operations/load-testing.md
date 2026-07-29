# Load testing

`scripts/load-test-5000.sh` runs a reproducible, local-only OpAMP scenario with
5,000 simulated collectors. It starts a unique Docker Compose project,
bootstraps a managed token through the admin API, and runs `cmd/opamp-load` from
the pinned `golang:1.25.12` image on that project's private network.

This is a capacity test, not a production diagnostic. Run it only on a host
with enough CPU, memory, file descriptors, and Docker resources for 5,000
concurrent WebSocket connections.

## Safety boundary

The script requires the exact `LOAD_TEST_CONFIRM=5000` acknowledgement. It
ignores inherited `DB_DSN`, `OPAMP_TOKEN_FILE`, and Compose selector variables,
then generates invocation-local JWT, PostgreSQL, first-admin, and managed
OpAMP token credentials under a private mode-`0700` temporary directory.
Credential files use mode `0600`.

Only the API is published, on a random loopback port for bootstrap. PostgreSQL
and the explicitly insecure local OpAMP listener remain on the unique Compose
network. The Go client receives the managed token through a read-only file
mount; no token value is passed through a process argument or server
environment variable.

Before logs can be used as evidence, every artifact is scanned with the token
file as a fixed-string pattern. A detected credential or a failed scan aborts
the run without printing contaminated logs. Cleanup revokes the public token
ID when possible and removes only the validated invocation-owned container,
Compose network, volumes, and temporary directory.

Do not point this scenario at a shared or production database, listener, or
secret.

## Run the scenario

From the repository root:

```bash
LOAD_TEST_CONFIRM=5000 ./scripts/load-test-5000.sh
```

The default ramp takes five minutes and the hold period takes ten minutes. Set
`LOAD_TEST_RAMP` and `LOAD_TEST_HOLD` to Go duration values for a shorter local
smoke while keeping the collector count fixed at 5,000:

```bash
LOAD_TEST_RAMP=1m \
LOAD_TEST_HOLD=5m \
LOAD_TEST_CONFIRM=5000 \
  ./scripts/load-test-5000.sh
```

`LOAD_TEST_READY_TIMEOUT_SECONDS` defaults to `900` and must be a positive
integer.

## Acceptance criteria and evidence

The script waits for a hold-phase JSON summary proving:

- exactly 5,000 clients attempted and connected;
- zero pre-connect failures, cancellations, stop failures, or interruptions;
- no disconnections before the hold phase;
- at most 40 application PostgreSQL connections;
- PostgreSQL major version 18;
- no application log line matching `error`, `failed`, or `panic`.

After the hold, the final summary must report exactly 5,000 clean
disconnections with the other failure counters still zero. The private
temporary artifact set includes the hold and final summaries, client
diagnostics, Compose logs, a Docker resource snapshot, and PostgreSQL activity.
It exists only for in-script validation and is destroyed during bounded
cleanup.

Successful standard output contains only safe aggregate evidence:

```text
5,000 collector load test completed
connection_establishment_seconds=<duration>
postgres_version=<PostgreSQL-18-version>
```
