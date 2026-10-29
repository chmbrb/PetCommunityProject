# 0011. Media processing pipeline

- **Status:** Proposed
- **Date:** 2026-10-29
- **Related:** [0006](0006-postgres-job-queue-river.md), [0010](0010-s3-compatible-media-storage.md), THREAT_MODEL.md P1, C1, C2, A2, D1

## Context

Uploaded images are the main route for malware (C2), illegal content (C1), and location leaks through EXIF metadata (P1). They also allow detection of stolen listing photos (A2). The threat model requires the pipeline to be finished before any upload feature ships.

## Decision

Every upload goes through this pipeline, and nothing is shown until it passes:

1. **Accept:** the API streams the upload to the `quarantine` bucket with a size limit (proposed: 15 MB) and creates a `MediaAsset` with status `pending`. Only JPEG, PNG, WebP, HEIC, and AVIF are accepted, detected by file content rather than extension. No SVG, GIF, or video in the MVP.
2. **Check dimensions:** the `media` job reads only the header and rejects images above a pixel limit (proposed: 40 megapixels) before decoding, which prevents decompression bombs.
3. **Decode and re-encode** with libvips (via `govips`) into fresh files: WebP in a few sizes (e.g. 320/800/1600 px) plus a JPEG fallback. All metadata is dropped (EXIF, XMP, ICC beyond sRGB conversion). Orientation is applied before stripping.
4. **Hash:** SHA-256 for exact duplicates; PDQ perceptual hash for near-duplicates.
5. **Check:**
   - Match against a CSAM hash list (provider to be chosen). A match quarantines the file without anyone viewing it, opens a highest-priority case, and starts the legal reporting runbook.
   - Compare against perceptual hashes of other listings' photos. A match from a different owner flags the listing for review (stolen photos).
6. **Publish:** processed files go to the `media` bucket, the asset becomes `ready`, and the raw upload is deleted.

The worker runs in its own container with a memory limit, read-only root filesystem, and no access beyond the database and object storage. Media is served from a separate hostname with `X-Content-Type-Options: nosniff` and a restrictive CSP.

## Consequences

- Originals are never served, so metadata leaks and polyglot files are neutralized by design.
- libvips needs cgo, so the worker image is larger than a static Go binary. Acceptable for the safety benefit.
- Uploads take a few seconds to appear. The UI shows a "processing" state.
- The CSAM hash source and its eligibility for a small nonprofit are still open (THREAT_MODEL.md follow-ups).

## Alternatives considered

- **Pure Go image libraries:** no cgo, but no WebP/AVIF encoding and weaker decoders.
- **ImageMagick:** history of serious vulnerabilities and higher resource use.
- **Hosted image service (Cloudinary etc.):** costs money and sends user photos to a third party.
