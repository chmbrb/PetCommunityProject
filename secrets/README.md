# Secrets

Passwords and keys for the production stack (`compose.yaml`). Each file is mounted
into only the containers that need it, at `/run/secrets/<name>`, and never appears in
an environment variable, so `docker inspect`, logs and crash reports can't leak it.
Everything in this folder except this README is ignored by git.

Create them with:

```sh
make secrets   # or: deploy/make-secrets.sh [DIR]
```

| File            | Contents                                                      | Used by           |
|-----------------|---------------------------------------------------------------|-------------------|
| `db_password`   | Database password (generated)                                 | db, app, worker, migrate |
| `secret_key`    | Encrypts actors' private signing keys (generated, ADR 0012)   | worker            |
| `backup_key`    | Encrypts backups (generated, ADR 0016)                        | backup            |
| `smtp_password` | Your email provider's SMTP password (may be empty)            | worker            |
| `s3_access_key` | Your storage provider's access key ID                         | app, worker, migrate |
| `s3_secret_key` | Your storage provider's secret key                            | app, worker, migrate |

- The script never overwrites a file. Changing `db_password` after the database was
  created doesn't change the database's password; change it in Postgres too.
- **Keep an offline copy of `backup_key` and `secret_key`**, away from this server and
  its backups. A restore needs both.
- The folder should be `0700` and the files `0444`: readable inside the containers
  (which run as unprivileged users), but not by other users on the host.
- To keep secrets elsewhere (another disk, a tmpfs filled by a secret manager), set
  `PC_SECRETS_DIR` in `.env`.
- Trailing newlines are ignored.
