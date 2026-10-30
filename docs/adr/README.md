# Architecture Decision Records

Short records of significant technical decisions: the context, what was decided, and the consequences. Format is based on Michael Nygard's ADR template (see [0001](0001-record-architecture-decisions.md)).

**Status values:** `Proposed` (recommended, awaiting confirmation) · `Accepted` · `Superseded by NNNN` · `Deprecated`

Decisions the project owner has already made in [PLAN.md](../PLAN.md) are `Accepted`. Implementation choices recommended during planning were `Proposed` until the owner confirmed them; all current ADRs are `Accepted`.

| # | Title | Status |
|---|-------|--------|
| [0001](0001-record-architecture-decisions.md) | Record architecture decisions | Accepted |
| [0002](0002-license-agplv3.md) | License under AGPLv3 | Accepted |
| [0003](0003-go-backend.md) | Go for the backend | Accepted |
| [0004](0004-postgresql-primary-datastore.md) | PostgreSQL as the primary datastore | Accepted |
| [0005](0005-data-access-sqlc-pgx-goose.md) | Data access with sqlc, pgx, and goose | Accepted |
| [0006](0006-postgres-job-queue-river.md) | Postgres-backed job queue (River) | Accepted |
| [0007](0007-vuetify-spa-with-server-rendered-public-pages.md) | Vuetify SPA with server-rendered public pages | Accepted |
| [0008](0008-caddy-reverse-proxy.md) | Caddy as the reverse proxy | Accepted |
| [0009](0009-docker-compose-deployment.md) | Docker Compose deployment | Accepted |
| [0010](0010-s3-compatible-media-storage.md) | S3-compatible object storage for media | Accepted |
| [0011](0011-media-processing-pipeline.md) | Media processing pipeline | Accepted (codecs superseded by 0021) |
| [0012](0012-activitypub-shaped-model-federation-deferred.md) | ActivityPub-shaped data model, federation deferred | Accepted |
| [0013](0013-identifiers-uuidv7-stable-uris.md) | UUIDv7 identifiers and stable URIs | Accepted |
| [0014](0014-authentication-sessions-mfa.md) | Authentication: server-side sessions and MFA | Accepted |
| [0015](0015-central-authorization-policy.md) | Central authorization policy layer | Accepted |
| [0016](0016-backups-wal-g.md) | Backups with WAL-G | Accepted |
| [0017](0017-instance-feature-flags.md) | Instance-level feature flags | Accepted |
| [0018](0018-mobile-apps-capacitor.md) | Native mobile apps with Capacitor | Accepted |
| [0019](0019-sign-in-options-passkeys-sso.md) | Sign-in options: passkeys, magic links, and Google/Apple SSO | Accepted |
| [0020](0020-shelter-software-imports.md) | Shelter software imports (Shelterluv and PetPoint) | Accepted |
| [0021](0021-memory-safe-image-codecs.md) | Memory-safe image codecs (pure Go and WebAssembly) | Accepted |

## Writing a new ADR

1. Copy [template.md](template.md) to `NNNN-short-title.md` using the next number.
2. Start with status `Proposed`. Change it to `Accepted` once agreed.
3. Never rewrite an accepted ADR's decision. Write a new ADR and mark the old one `Superseded by NNNN`.
4. Add it to the table above.
