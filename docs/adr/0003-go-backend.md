# 0003. Go for the backend

- **Status:** Accepted
- **Date:** 2026-10-29
- **Related:** PLAN.md (Stack, Principles: lean)

## Context

Instances should run cheaply on small servers and be easy for volunteers to operate. The backend handles an HTTP API, server-rendered pages, background jobs, and later ActivityPub federation (signature verification, delivery).

## Decision

Write the backend in Go as a single binary with several modes (`serve`, `worker`, `migrate`, admin commands). Use the standard library `net/http` router (Go 1.22+ pattern routing) and `html/template`, adding small focused libraries only where needed.

## Consequences

- Low memory use, fast startup, and one static binary make container images small and hosting cheap.
- Strong standard library for HTTP, crypto, and templating; good concurrency for federation delivery.
- The Go ActivityPub ecosystem is thin (`go-fed` is largely unmaintained). We'll implement the parts we need ourselves, using GoToSocial's source as a reference. This is extra work in Phase 5.
- Image processing needs libvips through cgo (see [0011](0011-media-processing-pipeline.md)), so the build isn't fully static for the worker.

## Alternatives considered

- **Node/TypeScript:** shared language with the frontend, but higher memory use and a larger dependency surface.
- **Rust:** excellent performance and safety, but a smaller contributor pool and slower development.
- **Ruby/Elixir:** proven for federated software (Mastodon, Pleroma), but heavier to host.
