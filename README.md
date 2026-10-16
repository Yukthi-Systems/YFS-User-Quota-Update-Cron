# YFS User Quota Update Cron

A small Go service that keeps the **main database** in sync with the storage usage recorded in the **Files database**.

On startup, and then every `SYNC_INTERVAL`, it:

1. Reads every user's used storage (bytes) from the Files database (`user_quotas` joined with `users` to get the email).
2. Converts bytes to **GB** (1 GB = 1024³ bytes), rounded to 2 decimals.
3. Updates `file_users.quota_utilized` in the main database, matching on email.

```
 Files DB (PostgreSQL)                         Main DB (PostgreSQL)
┌────────────────────────┐                    ┌──────────────────────────┐
│ users                  │                    │ file_users               │
│   user_id ─┐  email ───┼──── match email ──▶│   email                  │
│ user_quotas│           │                    │   quota_utilized (GB)    │
│   user_id ─┘           │   bytes / 1024³    │                          │
│   used_storage_bytes ──┼───────────────────▶│                          │
└────────────────────────┘                    └──────────────────────────┘
```

## Features

- **Runs on an interval:** a long-running service with no external cron needed.
- **Parallel batches:** updates go out in chunks across a pool of workers.
- **Skips unchanged rows:** rows that already have the correct value are not rewritten.
- **Logs missing emails:** emails that are in the Files database but not in `file_users` are logged as `missing_emails`.
- **Configured through the environment:** everything is set with environment variables, and no config file is needed.
- **Graceful shutdown:** stops cleanly on `SIGINT` / `SIGTERM` (`docker stop`).
- **Structured logs:** JSON logs go to the console, with an optional rotated log file.

## Database schema

### Files database (source, read-only)

```sql
CREATE TABLE users (
    user_id UUID PRIMARY KEY,
    email   VARCHAR(255) UNIQUE NOT NULL
);

CREATE TABLE user_quotas (
    user_id            UUID PRIMARY KEY REFERENCES users(user_id) ON DELETE CASCADE,
    used_storage_bytes BIGINT NOT NULL DEFAULT 0,
    used_file_count    INT    NOT NULL DEFAULT 0   -- not used by this service
);
```

Query used:

```sql
SELECT u.email, q.used_storage_bytes
FROM user_quotas q
JOIN users u ON u.user_id = q.user_id
WHERE u.email <> '';
```

### Main database (target, updated)

```sql
CREATE TABLE file_users (
    email          VARCHAR(254) PRIMARY KEY REFERENCES email_identities(email) ON DELETE CASCADE,
    quota_utilized NUMERIC(10,2) NOT NULL   -- in GB
);
```

The service only **updates** existing `file_users` rows. It never inserts or deletes rows.

### Required database permissions

| Database | Permission                                       |
|----------|--------------------------------------------------|
| Files    | `SELECT` on `users`, `user_quotas`               |
| Main     | `SELECT`, `UPDATE` on `file_users`               |

## Configuration

All configuration comes from environment variables. Copy [.env.example](.env.example) to `.env` and fill it in:

```bash
cp .env.example .env
```

| Variable               | Required | Default | Description                                                         |
|------------------------|:--------:|---------|---------------------------------------------------------------------|
| `FILES_PG_URL`         | ✅       |         | Connection URL of the Files database                                |
| `MAIN_PG_URL`          | ✅       |         | Connection URL of the main database                                 |
| `SYNC_INTERVAL`        |          | `5m`    | Time between runs (Go duration: `30s`, `5m`, `1h`, …)               |
| `FILES_PG_MAX_CONNS`   |          | `2`     | Max connections to the Files database                               |
| `FILES_PG_MIN_CONNS`   |          | `1`     | Min idle connections to the Files database                          |
| `MAIN_PG_MAX_CONNS`    |          | `5`     | Max connections to the main database (keep ≥ `PG_WORKERS`)          |
| `MAIN_PG_MIN_CONNS`    |          | `1`     | Min idle connections to the main database                           |
| `PG_UPDATE_CHUNK_SIZE` |          | `200`   | Rows per update batch                                               |
| `PG_WORKERS`           |          | `4`     | Number of parallel update workers                                   |
| `LOG_LEVEL`            |          | `info`  | `debug`, `info`, `warn`, `error`                                    |
| `LOG_SHOW_CONSOLE`     |          | `true`  | Write logs to stderr (visible in `docker logs`)                     |
| `LOG_FILE`             |          | *(empty)* | Path of a rotated log file; empty disables file logging           |
| `LOG_MAX_SIZE_MB`      |          | `250`   | Rotate the log file after this size                                 |
| `LOG_MAX_BACKUPS`      |          | `10`    | Number of rotated files to keep                                     |
| `LOG_MAX_AGE_DAYS`     |          | `20`    | Days to keep rotated files                                          |
| `LOG_COMPRESS`         |          | `true`  | Gzip rotated log files                                              |

Connection URL format:

```
postgresql://<user>:<password>@<host>:<port>/<database>?sslmode=disable
```

Use `sslmode=require` (or stricter) when the database requires TLS. If a password contains special characters (`@ : / ? #`), URL-encode them.

> [!WARNING]
> Never commit `.env`. It is already listed in `.gitignore`.

## Running

### Prerequisites

- Go **1.25+** to run locally, or Docker with the Compose plugin
- Network access from the machine or container to both PostgreSQL servers

### With Docker Compose (recommended)

```bash
cp .env.example .env        # then edit .env
docker compose pull
docker compose up -d
docker compose logs -f
```

Stop it:

```bash
docker compose down
```

[docker-compose.yml](docker-compose.yml) runs the published image `rjyspl/yfs-user-quota-update-cron:latest`, loads `.env` through `env_file`, and restarts the container automatically. To build from source instead, uncomment the `build:` section and run `docker compose up -d --build`.

### Publishing the image to Docker Hub

```bash
docker build -t rjyspl/yfs-user-quota-update-cron:latest .
docker push rjyspl/yfs-user-quota-update-cron:latest
```

The Docker Hub overview text is in [DOCKERHUB.md](DOCKERHUB.md). Paste it into the repository's **Overview** on hub.docker.com.

> [!NOTE]
> Inside a container, `localhost` refers to the container itself. To reach a database running on the host machine, use `host.docker.internal` as the host. On Linux, also add this to the service:
> ```yaml
> extra_hosts:
>   - "host.docker.internal:host-gateway"
> ```

### With Docker only

```bash
docker build -t yfs-user-quota-update-cron:latest .
docker run -d --name yfs-user-quota-update-cron --env-file .env --restart always yfs-user-quota-update-cron:latest
```

### Locally with Go (for testing)

The binary reads environment variables only. It does not load `.env` by itself, so export the file into your shell first:

```bash
cp .env.example .env        # then edit .env
set -a; source .env; set +a
go run ./cmd/app
```

Stop it with `Ctrl+C`. For quick testing, set `SYNC_INTERVAL=30s` and `LOG_LEVEL=debug`.

## Verifying it works

Expected log lines on each run (JSON):

| Message                                                      | Meaning                                          |
|--------------------------------------------------------------|--------------------------------------------------|
| `quota update service started`                               | Config loaded, both databases reachable          |
| `fetched N user quotas from files database`                  | Rows read from the Files database                |
| `quota chunk update completed`                               | One batch done (`updated_rows`, `unchanged_rows`) |
| `... with some emails not found in file_users`               | Lists `missing_emails` that have no `file_users` row |
| `quota update run completed`                                 | Whole run done, with `execution_time`            |

Check the result in the main database:

```sql
SELECT email, quota_utilized FROM file_users ORDER BY quota_utilized DESC LIMIT 20;
```

On the second run, `updated_rows` should be `0` unless usage changed in the meantime.

## Behaviour notes

- **Email matching is exact and case-sensitive.** `A@x.com` and `a@x.com` are different users.
- **Zero usage is written as `0.00`.** A user who deletes all their files goes back to 0 GB.
- **A negative `used_storage_bytes` is treated as `0`**, and a warning is logged.
- **Errors don't stop the service.** If a run fails (for example, a database is briefly unreachable), the error is logged and the next run happens at the next interval. If a database can't be reached **at startup**, the service exits, and Docker restarts it.
- **One failed batch doesn't stop the rest.** Each batch is a single statement, so when one fails it is logged and the other batches continue.
- **Precision:** `NUMERIC(10,2)` holds up to 99,999,999.99 GB.

## Project structure

```
.
├── cmd/app/main.go            # Entry point: config, DB pools, interval loop, shutdown
├── internal/
│   ├── config/                # Environment-variable configuration + validation
│   ├── filesdb/               # Reads user quotas from the Files database
│   ├── maindb/                # Batched, parallel updates of file_users
│   ├── pgpool/                # PostgreSQL connection pool helper (pgx)
│   ├── logger/                # zerolog + lumberjack log rotation
│   └── model/                 # Shared data types
├── Dockerfile                 # Multi-stage build → small Alpine image
├── docker-compose.yml
├── DOCKERHUB.md               # Docker Hub overview text
├── .env.example               # Template for .env
└── .github/workflows/ci.yml   # gofmt, go vet, build, docker build
```

## Development

```bash
gofmt -l .          # should print nothing
go vet ./...
go build ./...
```

GitHub Actions runs the same checks, plus a Docker build, on every push to `main` and on every pull request.

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `invalid configuration: FILES_PG_URL is required` | The variable isn't set. Check `.env`, and for local runs make sure you ran `set -a; source .env; set +a`. |
| `ping db: ... connection refused` / timeout | Check the host and port, firewall / `pg_hba.conf`, and `localhost` vs `host.docker.internal` inside Docker. |
| `password authentication failed` | Wrong credentials, or the password has special characters that need URL-encoding. |
| `permission denied for table ...` | Grant the permissions listed under [Required database permissions](#required-database-permissions). |
| Many `missing_emails` | Those emails have no row in `file_users`, or their case differs between the two databases. |
| `updated_rows` is always 0 | Values are already up to date, which is expected. |
