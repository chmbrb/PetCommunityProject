# 0009. Docker Compose deployment

- **Status:** Accepted
- **Date:** 2026-10-29
- **Related:** DESCRIPTION.md (Stack), PERSONAS.md (Instance operator), [0016](0016-backups-wal-g.md)

## Context

Instance operators are often volunteers with limited time and a single small server. Deployment, upgrades, and recovery must be simple and repeatable.

## Decision

Ship a `compose.yaml` as the supported deployment, with these services:

| Service | Image | Notes |
|---------|-------|-------|
| `caddy` | stock Caddy | Ports 80/443; the only service exposed to the internet |
| `app` | project image, `serve` mode | API + server-rendered pages |
| `worker` | same image, `worker` mode | Jobs, including media processing; memory-limited, read-only filesystem |
| `db` | PostGIS image | Private network only |
| `minio` | MinIO (optional) | Only if the operator doesn't use a hosted S3 provider |
| `backup` | WAL-G sidecar | Continuous WAL archiving and scheduled base backups |

Conventions:

- Images pinned by version and digest. The project publishes multi-arch images (amd64, arm64).
- Secrets passed as Docker secrets or an `.env` file that is never committed. The app refuses to start with default or missing secrets.
- Separate internal networks: the database and storage are unreachable from `caddy`.
- Upgrades: `docker compose pull && docker compose up -d`. Migrations run automatically via `app migrate` before `serve`.
- Health checks on every service.

## Consequences

- One documented path to a running instance. Easy to test in CI.
- Single-host only. Scaling beyond one server (Kubernetes etc.) is out of scope until an instance actually needs it.
- Compose files are part of the public interface; changes need upgrade notes.

## Alternatives considered

- **Kubernetes/Helm:** too heavy for the target operator.
- **Bare-metal install scripts:** hard to support across distributions.
- **Single all-in-one container:** simpler to start, but mixes privileges and complicates upgrades.
