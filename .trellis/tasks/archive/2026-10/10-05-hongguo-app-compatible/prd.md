# HongGuo App compatible media fallback

## Requirement
When the App returns only ByteVC2 variants, resolve the same video's highest compatible variant through its official fplay endpoint before the existing worker switches sources. Preserve normal App requests, source order, checkpoints, public metadata and manual retries.

## Acceptance criteria
- Compatible App responses make no extra request; business, malformed and cancelled responses do not invoke the compatibility path.
- Official fplay uses a validated fixed host/path, removes force_fids and requests codec_type=1. Its video identity must match the original App model.
- Decode the verified version-1 URL envelope using bounded Base64, SHA-512 key derivation, AES-128-CBC and strict padding; reject unknown versions and unsafe URLs.
- Highest compatible candidates reuse existing key/quality parsing and return DownloadMedia under source app. Key re-resolution follows the same path.
- Focused regression/race tests and an opt-in full decode of season six episode 138 pass without changing the production queue.
- No new dependencies, settings, schema or frontend changes; no upstream URLs or keys in logs/fixtures.
