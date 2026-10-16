# YFS User Quota Update Cron

A lightweight Go service that syncs users' storage usage from the **YFS Files database** to the **main database**.

On startup, and then every `SYNC_INTERVAL`, it:

1. Reads each user's `used_storage_bytes` from the Files database (`user_quotas` joined with `users` to get the email).
2. Converts bytes to **GB** (1 GB = 1024³ bytes), rounded to 2 decimals.
3. Updates `file_users.quota_utilized` in the main database, matching on email.

**Highlights**

- Runs on its own interval, with no external cron needed
- Parallel batched updates; rows that haven't changed are skipped
- Configured entirely through environment variables, with no config file
- JSON logs to `docker logs`, plus an optional rotated log file
- Graceful shutdown on `docker stop`
- Small Alpine-based image with a static binary

## Quick start

```bash
docker run -d \
  --name yfs-user-quota-update-cron \
  --restart always \
  -e SYNC_INTERVAL=5m \
  -e FILES_PG_URL='postgresql://user:password@files-db-host:5432/files_db?sslmode=disable' \
  -e MAIN_PG_URL='postgresql://user:password@main-db-host:5432/main_db?sslmode=disable' \
  rjyspl/yfs-user-quota-update-cron:latest
```

Follow the logs:

```bash
docker logs -f yfs-user-quota-update-cron
```

## Docker Compose

`docker-compose.yml`:

```yaml
services:
  yfs-user-quota-update-cron:
    image: rjyspl/yfs-user-quota-update-cron:latest
    container_name: yfs-user-quota-update-cron
    env_file:
      - .env
    restart: always
```

`.env`:

```dotenv
SYNC_INTERVAL=5m
FILES_PG_URL=postgresql://user:password@files-db-host:5432/files_db?sslmode=disable
MAIN_PG_URL=postgresql://user:password@main-db-host:5432/main_db?sslmode=disable
LOG_LEVEL=info
```

Run it:

```bash
docker compose pull
docker compose up -d
docker compose logs -f
```

## Environment variables

| Variable               | Required | Default   | Description                                                   |
|------------------------|:--------:|-----------|---------------------------------------------------------------|
| `FILES_PG_URL`         | yes      |           | Connection URL of the Files database (source)                 |
| `MAIN_PG_URL`          | yes      |           | Connection URL of the main database (target)                  |
| `SYNC_INTERVAL`        |          | `5m`      | Time between runs (Go duration: `30s`, `5m`, `1h`, …)         |
| `FILES_PG_MAX_CONNS`   |          | `2`       | Max connections to the Files database                         |
| `FILES_PG_MIN_CONNS`   |          | `1`       | Min idle connections to the Files database                    |
| `MAIN_PG_MAX_CONNS`    |          | `5`       | Max connections to the main database (keep ≥ `PG_WORKERS`)    |
| `MAIN_PG_MIN_CONNS`    |          | `1`       | Min idle connections to the main database                     |
| `PG_UPDATE_CHUNK_SIZE` |          | `200`     | Rows per update batch                                         |
| `PG_WORKERS`           |          | `4`       | Number of parallel update workers                             |
| `LOG_LEVEL`            |          | `info`    | `debug`, `info`, `warn`, `error`                              |
| `LOG_SHOW_CONSOLE`     |          | `true`    | Write logs to stderr (shown by `docker logs`)                 |
| `LOG_FILE`             |          | *(empty)* | Path of a rotated log file inside the container; empty = off  |
| `LOG_MAX_SIZE_MB`      |          | `250`     | Rotate the log file after this size                           |
| `LOG_MAX_BACKUPS`      |          | `10`      | Number of rotated files to keep                               |
| `LOG_MAX_AGE_DAYS`     |          | `20`      | Days to keep rotated files                                    |
| `LOG_COMPRESS`         |          | `true`    | Gzip rotated log files                                        |

Connection URL format:

```
postgresql://<user>:<password>@<host>:<port>/<database>?sslmode=disable
```

Use `sslmode=require` (or stricter) if the database requires TLS. URL-encode any special characters in the password (`@ : / ? #`).

### Writing logs to a file

```bash
docker run -d \
  --name yfs-user-quota-update-cron \
  --env-file .env \
  -e LOG_FILE=/app/logs/quota-update.log \
  -v "$(pwd)/logs:/app/logs" \
  rjyspl/yfs-user-quota-update-cron:latest
```

## Required database schema

**Files database (source, read-only):**

```sql
CREATE TABLE users (
    user_id UUID PRIMARY KEY,
    email   VARCHAR(255) UNIQUE NOT NULL
);

CREATE TABLE user_quotas (
    user_id            UUID PRIMARY KEY REFERENCES users(user_id) ON DELETE CASCADE,
    used_storage_bytes BIGINT NOT NULL DEFAULT 0,
    used_file_count    INT    NOT NULL DEFAULT 0
);
```

**Main database (target):**

```sql
CREATE TABLE file_users (
    email          VARCHAR(254) PRIMARY KEY REFERENCES email_identities(email) ON DELETE CASCADE,
    quota_utilized NUMERIC(10,2) NOT NULL   -- in GB
);
```

**Permissions needed:**

| Database | Permission                          |
|----------|-------------------------------------|
| Files    | `SELECT` on `users`, `user_quotas`  |
| Main     | `SELECT`, `UPDATE` on `file_users`  |

The service only **updates** existing `file_users` rows. It never inserts or deletes rows.

## Behaviour

- **Email matching is exact and case-sensitive.**
- **Emails that are in the Files database but not in `file_users` are logged as `missing_emails`.**
- **Zero usage is written as `0.00`.** A negative byte count is treated as `0`.
- **Failures during a run don't stop the service.** If a run fails, the error is logged and the next run happens at the next interval.
- **If a database can't be reached at startup, the container exits.** Use `--restart always` (or `restart: always` in Compose) so it retries.

## Logs

Example log messages (JSON):

| Message                                         | Meaning                                              |
|-------------------------------------------------|------------------------------------------------------|
| `quota update service started`                  | Config loaded and both databases reachable           |
| `fetched N user quotas from files database`     | Rows read from the source                            |
| `quota chunk update completed`                  | One batch done (`updated_rows`, `unchanged_rows`)    |
| `... with some emails not found in file_users`  | Lists `missing_emails`                               |
| `quota update run completed`                    | Whole run done, with `execution_time`                |

## Networking notes

Inside the container, `localhost` refers to the container itself. To reach a PostgreSQL server running on the Docker host, use `host.docker.internal`. On Linux, also add:

```bash
--add-host=host.docker.internal:host-gateway
```

or, in Compose:

```yaml
extra_hosts:
  - "host.docker.internal:host-gateway"
```

## Troubleshooting

| Symptom                                           | Fix                                                                    |
|---------------------------------------------------|------------------------------------------------------------------------|
| `invalid configuration: FILES_PG_URL is required` | Set the variable with `-e` or `--env-file`                             |
| `ping db: ... connection refused` / timeout       | Check host and port, firewall / `pg_hba.conf`, and `host.docker.internal` |
| `password authentication failed`                  | Wrong credentials, or the password needs URL-encoding                  |
| `permission denied for table ...`                 | Grant the permissions listed above                                     |
| Many `missing_emails`                             | Those emails have no row in `file_users`, or their case differs        |

## Image details

- **Base image:** `alpine`
- **Binary:** `/app/updater` (statically linked Go binary, the entrypoint)
- **Ports:** none exposed, because the service makes only outbound database connections
- **Volumes:** none required; mount `/app/logs` only if you use `LOG_FILE`
