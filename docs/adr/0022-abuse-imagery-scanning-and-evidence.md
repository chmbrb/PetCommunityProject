# 0022. Abuse imagery scanning and evidence preservation

- **Status:** Accepted
- **Date:** 2026-11-05
- **Details:** the "Match against a CSAM hash list" step in [0011](0011-media-processing-pipeline.md). The rest of 0011 stands.
- **Related:** [0010](0010-s3-compatible-media-storage.md), [0011](0011-media-processing-pipeline.md), [0012](0012-activitypub-shaped-model-federation-deferred.md), THREAT_MODEL.md C1, [COMPLIANCE.md](../COMPLIANCE.md#phase-1-foundation) Phase 1, PLAN.md Data Retention

## Context

Before uploads can be open to everyone, each one has to be checked against known child sexual abuse material (CSAM). In the US, a provider that learns of a match must report it to NCMEC's CyberTipline and preserve the material and related information for at least one year after the report (18 U.S.C. § 2258A, as amended by the REPORT Act of 2024).

The pipeline from 0011 had a scanner step with no provider behind it. On a match it marked the upload `quarantined` and left the original in the quarantine bucket. Two things defeated the preservation:

- `.env.example` tells operators to give the quarantine bucket a lifecycle rule that deletes objects after a day, so the original would expire.
- `media_assets` rows are deleted with their owner's actor, so deleting the account would remove the record.

The hash-matching provider isn't chosen yet. PhotoDNA, Cloudflare's CSAM scanning tool, and NCMEC's hash sharing each require registration, and the project has no legal entity yet (PLAN.md, Open Questions). The design must let a provider be added without changing what happens after a match.

## Decision

**A small scanner interface, chosen by configuration.** `media.Scanner` has `Name()` and `Scan(original) → {verdict, reference}`. The reference identifies what matched (a hash, or the provider's tracking ID), never image content. Scanners fail closed: an error retries the job, and an upload that never gets a verdict is rejected by the stale-upload cleanup, never published. `PC_ABUSE_SCANNER` selects one:

- `none` (default): no scanning. Uploads stay limited to accounts with a confirmed email, and the worker logs a warning at startup.
- `hashlist`: exact matching against a file of MD5, SHA-1, or SHA-256 digests (`PC_ABUSE_HASH_LIST_FILE`). It's the form in which NCMEC and others share hash lists with registered providers, and it lets the match path be tested end to end. Exact hashes miss any resized or re-encoded copy, so it's a minimum, not the final answer.

A perceptual provider (PhotoDNA, or PDQ against a shared list) is added as another scanner when one is available to the project.

**On a match, in this order:**

1. The original is encrypted (AES-256-GCM, key derived from `PC_SECRET_KEY`, bound to the upload's ID) and stored in a separate, private **evidence bucket** (`PC_S3_EVIDENCE_BUCKET`), with no lifecycle rule. It's required whenever a scanner is configured. Nothing is decoded, published, or logged.
2. In one transaction:
   - the upload is marked `quarantined`;
   - an `abuse_imagery_cases` row records the upload, the owner's actor and account, the scanner and its reference, the original's SHA-256, and the evidence location;
   - the owner's account is `suspended` and all its sessions are revoked;
   - both actions are written to a new append-only `audit_log`.
3. The raw file is deleted from quarantine, leaving only the encrypted copy.

A retried job finds the case already open and does nothing more.

**Preservation is enforced by the database.** The case references the upload, the actor, and the account with `ON DELETE RESTRICT`, so none of them can be deleted while the case exists. A case moves from `open` to `reported` (with the report's reference) or `closed`. A `reported` case must have `preserve_until` at least one year after `reported_at`.

## Consequences

- No provider is connected yet. Until one is, the email-confirmation limit on uploads stays.
- Operators who turn on a scanner need a third bucket, configured not to expire objects.
- Evidence is unreadable without `PC_SECRET_KEY`. Losing the key loses the evidence, which is one more reason to keep it backed up offline (PLAN.md Reminders).
- A suspended account can't sign in. Its other content stays visible until moderation tools exist.
- Follow-up work:
  - **Operator tooling** (done): `petcommunity cases` lists cases, exports a case's evidence for the report (checked against its SHA-256, refused to a terminal), records the report's reference and preservation date, and closes cases, writing each export, report, and close to the audit log with the operator's name (DEVELOPMENT.md, "Abuse imagery cases").
  - **Notifying the operator:** today a match is only an error-level log line.
  - **Upload IP addresses:** a CyberTipline report asks for the uploader's IP address and upload time. Uploads don't record an IP address yet, and server logs are kept only briefly (PLAN.md Data Retention). Decide whether to record it per upload.
  - **The incident response runbook** (done, as a draft for legal review): [INCIDENT_RESPONSE.md](../INCIDENT_RESPONSE.md), "Abuse imagery". The operator files the report, with a target of 24 hours from learning of a match.
  - **Purging after preservation** (done): a daily worker job deletes the evidence and the case for closed cases past `preserve_until`, and no sooner than 30 days after closing, so a case closed by mistake can still be exported. Each purge is written to the audit log; the quarantined upload's record stays.

## Alternatives considered

- **Keep the evidence in the quarantine bucket under a separate prefix:** fewer buckets, but a bucket-wide lifecycle rule, which is what `.env.example` asked operators to set, would silently delete it.
- **Store evidence unencrypted:** simpler to report from, but anyone who obtains the bucket's credentials or contents would hold the material itself.
- **Flag only, without suspending the account:** leaves the uploader able to keep uploading, and to delete their account, while the report is prepared.
- **Wait for a perceptual provider before building anything:** the case handling, evidence storage, and retention rules don't depend on the provider, and the legal process takes longer than the code.
