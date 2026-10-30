# 0021. Memory-safe image codecs (pure Go and WebAssembly)

- **Status:** Accepted
- **Date:** 2026-10-30
- **Supersedes:** the choice of libvips in [0011](0011-media-processing-pipeline.md) (decode and re-encode step). The rest of 0011 stands.
- **Related:** [0011](0011-media-processing-pipeline.md), [0003](0003-go-backend.md), [0009](0009-docker-compose-deployment.md), THREAT_MODEL.md C2

## Context

ADR 0011 chose libvips (C, via cgo) to decode and re-encode uploads. Uploaded images are hostile input, and image decoders are a classic source of memory-corruption bugs. With libvips, a bug in a C decoder can corrupt the worker's memory. Using cgo also adds build complexity: every place that compiles the Go module needs libvips headers (developer machines, CI), and the app image would grow from a 15 MB static image to a glibc-based one with C libraries. Build tags or containerized development could contain that, at the cost of extra build configuration.

Since 0011 was written, the gen2brain codecs (WebP, AVIF, HEIC) have become available as C libraries compiled to WebAssembly and transpiled to Go source ("wasm2go"), so they run as ordinary, memory-safe Go code.

## Decision

Decode and re-encode images with memory-safe code only:

- **JPEG, PNG:** Go's standard library decoders. JPEG EXIF orientation is read by a small, bounds-checked, fuzz-tested parser in `internal/media`.
- **WebP (decode and encode), AVIF, HEIC (decode):** `github.com/gen2brain/webp`, `/avif`, `/heic`, using their transpiled-Go backends.
- **Resizing:** `golang.org/x/image/draw` (a bilinear pre-pass, then Catmull-Rom).
- **No C shared libraries.** The gen2brain packages load a system C library (libwebp, libavif, libheif) when one is installed, and only fall back to the Go build otherwise. Every build uses the `nodynamic` tag to disable that: the Makefile (`GOFLAGS`), the Dockerfile, CI, and golangci-lint. As a second line of defense, the worker calls `media.CheckPureCodecs()` at startup and refuses to run if any codec loaded a C library; a unit test does the same.
- The codecs sit behind `media.Process`, so libvips could be swapped in later if speed or library health requires it.

## Consequences

- Parsing hostile files stays within Go's memory safety.
- Plain `go build` / `go test` work everywhere with no system packages, and the app and worker stay a single static binary in a distroless image.
- Slower than libvips: a few seconds for a large photo, which is fine at this volume. Under the race detector, the media tests take about 50 seconds.
- The gen2brain libraries are smaller, single-maintainer projects. Dependabot tracks them; if they stall, the interface makes switching straightforward.
- Forgetting the `nodynamic` tag on a machine with C image libraries installed is caught at worker startup and in tests, not silently ignored.

## Alternatives considered

- **libvips via cgo (ADR 0011):** fast and mature, but C code parsing untrusted input, plus cgo build complexity everywhere the module compiles.
- **libvips only inside container builds (build tags):** contains the build cost, but keeps C decoders and splits the code into parts plain `go test` can't exercise.
- **Standard library only:** memory-safe, but no WebP/AVIF/HEIC support, and phones produce HEIC.
