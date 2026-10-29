# 0020. Shelter software imports (Shelterluv and PetPoint)

- **Status:** Accepted
- **Date:** 2026-10-29
- **Related:** PLAN.md (Phase 0 findings, Phase 2), DOMAIN_MODEL.md (Animal, Listing, ImportSource), [0006](0006-postgres-job-queue-river.md), [0011](0011-media-processing-pipeline.md), THREAT_MODEL.md A9, A10, I4

## Context

Phase 0 outreach found that local shelters use a mix of **Shelterluv** and **PetPoint**. Shelter staff have little time, so they won't re-enter animals by hand. Importing from their existing software is the main way to get listings onto the platform.

Shelter software holds much more than adoptable animals: intake and surrender records, finder and owner details, adopter records, medical notes, and staff notes. None of that belongs on the platform.

What's known about access (from third-party sources; confirm with each vendor):

- **Shelterluv:** a shelter admin generates an API key in Shelterluv's settings (reported path: Configuration → Uploads & Integrations → API). Endpoint details need to come from Shelterluv's documentation or support.
- **PetPoint:** web services are off by default. The shelter asks PetPoint support to enable them, then copies an authorization key from its admin settings. The adoption web service is a SOAP-style service at `ws.petango.com`; available methods and formats need confirming.

## Decision

- **Both integrations are supported.** **Shelterluv first** (Phase 2), PetPoint next. A CSV template is the fallback for shelters on other software.
- **Read-only, animal data only.** The platform only reads. It never writes back to the shelter's software.
- **Allow-list field mapping.** Each integration maps a fixed list of animal fields (name, species, breed, sex, age, size, color, spay/neuter, good-with, adoption fee, status, public description, photos). Everything else in the response is discarded before it's stored or logged, including:
  - any person record (intake source, finder, surrendering owner, adopter, foster, staff)
  - internal notes, medical records, behavior notes not marked public
  - addresses and contact details
  - kennel or internal location IDs
- **Free-text screening.** Imported public descriptions are scanned for phone numbers, email addresses, street addresses, and names that match a person field from the same record. Matches are held as drafts for the shelter to review instead of being published.
- **Photos** go through the normal media pipeline ([0011](0011-media-processing-pipeline.md)), so metadata is stripped.
- **Credentials:** the shelter enters its own API key, which is stored encrypted, used only by the import worker, and can be removed by the shelter at any time. The UI asks for the most limited key the vendor offers.
- **Sync:** a scheduled River job per shelter (proposed: every 30 minutes, plus a manual "sync now"). Status changes in the shelter software (adopted, on hold, removed) update the listing. Animals that disappear from the feed are withdrawn.
- **Source of truth:** the shelter software owns imported fields; those fields are read-only on the platform. Platform-only fields (custom application questions, extra photos) stay editable.
- **Audit:** every sync run records counts (added, updated, withdrawn, held for review) and errors, without storing raw responses.

## Consequences

- Shelters can list all their adoptable animals without re-entering them.
- The platform never holds shelter PII, which reduces breach impact and legal exposure.
- Two vendor integrations to maintain. Vendor changes can break imports silently, so sync failures alert the shelter and appear on the admin dashboard.
- Vendor terms may restrict how API data is used or shown. Check Shelterluv's and PetPoint's terms before Phase 2.
- Allow-list mapping means new vendor fields are ignored until deliberately added. This is intentional.

## Alternatives considered

- **Manual entry only:** shelters won't do it.
- **Import via Petfinder or RescueGroups feeds:** one integration covers many shelters, but adds a middleman with its own terms. Revisit if direct integrations stall.
- **Mirror the full vendor record and filter on display:** simpler mapping, but stores PII the platform doesn't need.
