# Independent review and validation

- Reviewed the resolver diff, new compatibility decoder/tests and opt-in service pipeline independently. No changes to worker source order, retry budget, schema, settings or public DTOs.
- Review raised malformed entries mixed with ByteVC2 as a possible trigger gap. Kept the approved nonempty **every candidate explicitly ByteVC2** contract: unclassified/bad compatible entries are not evidence of all-ByteVC2. Added both mixed-list regression cases; normal parsing failures continue existing source rotation.
- `go test -race ./internal/hongguo -count=1` passed; `go vet ./internal/hongguo` passed. Synthetic fixtures contain no real upstream addresses, credentials or media keys.
- Isolated PostgreSQL service tests passed under race detection: full verification, separate verification/recovery, source priority/fallback and three-source order.
- First live test exposed incomplete test setup: a single episode row triggered existing whole-work reconciliation because the current series has later episodes. Added an isolated completed-tail fixture so only episode 138 transfers; no production behavior changed.
- Live episode 138 completed through the production resolver, independent key re-resolution, decryption/remux, FFprobe, full decode, hashing and publication. Actual result: App source, HEVC, 1920x1080, quality 1080. Live regression additionally counts the two official fplay requests and requires encryption, one source attempt and full verification enabled.
- No production queue retries, deployment/service restart, cloud integration or Emby playback verification. Future upstream host/envelope changes fail safely into existing bounded fallback.
- Preserve unrelated Emby/work-query edits. The user authorized committing and pushing this task; stage only its code, tests, contract and task records.

- Strengthened live assertions passed on the final run under race detection (6.47s): exactly two compatibility calls, encrypted input, one source attempt, full verification enabled. An earlier run completed but failed the strict counters before diagnostic fields were added; those counts were unavailable, so the cause was not established. Live upstream/network variability remains a validation limitation.
