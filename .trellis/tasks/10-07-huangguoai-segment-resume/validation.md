# Validation

## Implemented boundary
- Source-local opt-in completion records and copying into each new lease directory.
- Snapshot persisted before database staging_path checkpoint; failed snapshot/Resolve retains the old reference.
- Fingerprint includes full parsed playlist/Referer and newly fetched map/key contents; no raw URLs or keys in completion records.
- Transfer failures retain complete segments only; merge/full verification failures discard caches. No Range fallback or production mutation.

## Checks
- Full source package race: passed, 9.188 seconds (includes actual synthetic HLS merging/full decoding).
- Isolated PostgreSQL 16 service targeted race: passed, 13.601 seconds. Includes new failed-transfer -> failed-Resolve -> retry -> missing-segment-only -> independent verification -> publication -> cleanup regression, existing decode/duration/cancel/publish tests and all four category paths/legacy retry.
- Isolated PostgreSQL repository Movie binding/legacy S01E001 regression: passed, 4.234 seconds.
- Direct-source-change regression under race: passed, 1.040 seconds.
- go vet source/service and git diff --check: passed.
- Test database was a temporary container with localhost-only port, stopped and removed after checks, never production. No live upstream download in this task.

## Independent review
- Source cache reviewer checked identity/size/digest/worker join and requested a blocked-sibling cancellation test; added and passed.
- Worker reviewer found a genuine premature checkpoint bug; corrected to copy+fsync then fenced DB callback before retiring previous stage. Snapshot cancel/collision/callback failure regression added.
- Final reviewer checked corrected handoff and noted HLS->direct failure discards old cache. Assessed against approved source-change invalidation: intentional behavior, no mixed-source reuse. Added explicit direct-source-change regression and documented this compatibility rule.

## Limits / delivery
- Code and verification records are delivered in the task commit; not deployed. Production queue/service unchanged.
- No live upstream repeated-signature or production crash/power-loss test. Signed URL changes invalidate conservatively.
- Failed/cancelled tasks now consume cache disk; a retry temporarily holds the old and new snapshots until durable handoff. Existing deleted historical segments cannot be recovered.
- Existing untracked core is outside this task and untouched.
