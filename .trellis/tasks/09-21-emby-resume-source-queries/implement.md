# Implementation

1. Add source-specific resume candidates and count/group/bounded merge pipeline; preserve SQL sort and filters.
2. Route global IsResumable to the new pipeline and share current-page payload loading.
3. Run isolated PostgreSQL mixed-source regressions, compare with the prior query, and inspect EXPLAIN ANALYZE on a large unrelated catalog.
4. Run existing resume/catalog tests, go vet for the affected package and git diff --check; independently review changes.
5. Document the verified query boundary and report unverified production timing.

The user approved the final proposal in the preceding conversation. Existing unrelated worktree changes are not part of this task. Do not commit or start the application server.

## Verification

- Isolated UTF-8 PostgreSQL 15: mixed-source behavior and original-query comparison passed, including deep offsets, overflow, empty pages, filters, versions, album seasons, restricted/hidden libraries, ancestor NSFW, NULL/time ties, soft-deleted history and removed files.
- Each source with 20,000 catalog items and 20,000 unrelated-user states: EXPLAIN ANALYZE verified indexed candidate access without whole-catalog or whole-state scans.
- Existing ContinueWatching, Emby Items IsResumable, HongGuo Emby, NFO fresh startup/hierarchy and shared metadata visibility tests passed.
- `go vet ./internal/service` and `git diff --check` passed.
- Independent read-only review: strengthened state-table plan coverage and fixed expected IDs for NULL/time ties. Rejected suggested album-container output: original IsResumable explicitly excludes Series and preserves playable Episode output.
- No HTTP contract/catalog change: routes, params, payload loader and Fields behavior retained. Production data/latency and device playback were not tested.
