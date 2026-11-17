# 0025. Rotating the instance's encryption keys

- **Status:** Accepted
- **Date:** 2026-11-17
- **Related:** [0012](0012-activitypub-shaped-model-federation-deferred.md) (actor keys), [0014](0014-authentication-sessions-mfa.md) (authenticator secrets), [0016](0016-backups-wal-g.md) (backups), [0022](0022-abuse-imagery-scanning-and-evidence.md) (evidence); INCIDENT_RESPONSE.md, "Data breach or server compromise"; THREAT_MODEL.md I5

## Context

Two keys encrypt data at rest, and neither can be changed today: changing one makes everything it encrypted unreadable (DEVELOPMENT.md, INCIDENT_RESPONSE.md).

| Key | Held by | Encrypts | Where the data is | Records which key? |
|---|---|---|---|---|
| `PC_SECRET_KEY` (`secret_key`) | the worker | actors' private signing keys | `actors.private_key_enc` | yes: `private_key_kid` |
| `PC_SECRET_KEY` | the worker | abuse imagery evidence | the evidence bucket | no: only a format version byte |
| `mfa_key` (`PC_MFA_KEY`) | the API | authenticator (TOTP) secrets | `totp_factors.secret_encrypted` | yes: `key_id` |

Each key is only used to encrypt, with AES-256-GCM under a key derived by HKDF, and each sealed value is bound to its row's ID. Nothing is signed or hashed with them, so rotating means re-encrypting, nothing more. Where a key ID is stored, it's derived from the key (8 bytes of HKDF output), so it identifies the key without revealing it, and opening refuses a value sealed under any other key.

A rotation is needed after a suspected leak, when someone who had a copy of the key leaves, and as routine hygiene. It has limits worth stating up front: data an attacker already copied along with the key stays exposed, and rotating only protects what they don't have yet.

Two things outlive a rotation:

- **Database backups** keep the old ciphertexts. Restoring one taken before a rotation brings back data that only the old key can read. Backups keep the last 7 daily base backups by default (`PC_BACKUP_RETAIN_FULL`, ADR 0016), and off-site copies, once they exist, may be kept longer.
- **The evidence bucket** may keep old object versions, since `.env.example` recommends versioning or object lock for it. Re-encrypting writes a new version; the old one stays, readable with the old key, for as long as the bucket keeps it.

## Decision

**A current key and an optional previous key, for each of the two.**

- `PC_SECRET_KEY` and `PC_MFA_KEY` stay as they are: the current key, which seals everything new.
- New settings `PC_SECRET_KEY_PREVIOUS` and `PC_MFA_KEY_PREVIOUS` (files `secret_key_previous` and `mfa_key_previous`, mounted into the same containers as the current keys) hold the key being retired. They're only used to open values sealed under it.
- Each previous key must be 32 bytes, differ from the current one, and, in production, not be a development key. Only one previous key: a rotation finishes before the next one starts.

**Every sealed value says which key sealed it.** The sealers become keyrings: they seal with the current key and record its key ID, and open with whichever key the stored ID names. A value under an ID that's neither key fails as it does today (`ErrWrongSecret`, `ErrMFAKeyMismatch`). Actor keys and authenticator secrets already store the ID. Evidence gets a new sealed format, version 2: `[2][8-byte key ID][nonce][ciphertext]`. Version 1 files, which have no ID, are opened by trying the current key and then the previous one; AES-GCM fails cleanly with the wrong key, so there's no ambiguity.

**Re-encryption is an explicit command, run where each key is.**

- `petcommunity keys status` counts, for each kind of data the process can see, how many values are under the current key, the previous key, or neither.
- `petcommunity keys reencrypt` opens every value sealed under the previous key and seals it again under the current one, in small batches, each in its own transaction (for evidence: read, re-seal, and overwrite the object). It's safe to stop and run again, and it finishes by printing `keys status`.
- Each runs in the container that holds the key: the worker for `PC_SECRET_KEY` (actor keys and evidence), the API's container for `mfa_key` (authenticator secrets). A command that needs a key it doesn't have says so and does nothing.
- No automatic background job: rotation is rare, and an operator should see it finish.

**The rotation procedure** (DEVELOPMENT.md and the runbook will carry it):

1. Generate a new key (`openssl rand -base64 32`).
2. Move the current key's file to `*_previous`, and put the new key in its place.
3. Restart the services that hold it (the worker, or the app). Everything new is sealed under the new key; everything old still opens.
4. Run `keys reencrypt` in that container, until `keys status` shows nothing left under the previous key.
5. Remove `*_previous` and restart again.
6. **Keep the retired key offline** until every backup taken before the rotation has expired: by default a week, longer for any off-site copies. Restoring such a backup needs it: put it back as `*_previous`, restore, and run `keys reencrypt` again. Then destroy it.

**After a leak,** the procedure is the same, done straight away. Also delete old versions in the evidence bucket if its settings allow, and treat what the old key protected as exposed (INCIDENT_RESPONSE.md).

## Consequences

- Both keys can be rotated without downtime or data loss, and the runbook's breach playbook can say so.
- Rotation doesn't reach copies made before it: old backups, and old evidence object versions in a versioned or locked bucket, stay readable with the old key until they expire. That's why the retired key is destroyed only once they have.
- Evidence moves to sealed format version 2. Version 1 stays readable until every file is re-encrypted.
- `backup_key` (WAL-G's) is outside this decision: new backups can use a new key at any time, but each old backup needs the key it was made with. Rotating it is a separate procedure, with the off-site backup work.
- Built: the keyrings, evidence format version 2, `petcommunity keys status` and `keys reencrypt`, the `*_previous` files in `compose.yaml` and `make secrets`, and the procedure in DEVELOPMENT.md ("Rotating encryption keys") and INCIDENT_RESPONSE.md.

## Alternatives considered

- **Envelope encryption (a data key per value, wrapped by the instance key):** rotating then only rewraps small data keys, which matters for large data sets. The data here is a few rows and files per instance, so re-encrypting directly is simpler, with one less layer to get wrong.
- **An external key manager (Vault, a cloud KMS):** keeps keys off the server, but adds a service to run or pay for, against the project's aim of being cheap to self-host (PLAN.md, Principles).
- **Re-encrypting lazily, when each value is next used:** needs no command, but a value that's never used again, such as an inactive account's authenticator secret, would stay under the old key for good, so the old key could never be retired.
- **A list of previous keys:** allows overlapping rotations, which aren't needed, and makes it harder to tell when a key can be destroyed.
- **Re-encrypting automatically in a background job:** a rotation would finish without anyone seeing it, and the API has no job runner for its own key.
