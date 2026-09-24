# Design

## Goal and boundaries

Remove the two request-time HongGuo costs identified in diagnosis: global visible-identity enumeration and broad candidate revalidation. Keep all three sources, logical identities, search semantics and final-page card loading. The earlier parallel task remains unchanged and uncommitted.

The minimal approach reuses `MetadataSearchDocument.LibraryIDs`, the existing OpenSearch library filter, the HongGuo `searchIndex` coordinator and current mutation transaction owners. Do not add a cache, broker, polling worker, schema table or generic search framework.

## Document projection

1. Keep one document per existing logical `hg-work-` / `hg-group-` identity. Official series albums include all eligible member seasons; standalone series and movies retain their current identities.
2. Resolve a bounded requested identity batch to source works using work IDs or `related_album_id` predicates before album/title and file work. Do not filter only a computed identity after walking the entire catalog. This bounded resolution supports both document refresh and indexed-hit revalidation, while the unbounded fallback remains separate.
3. Derive a sorted, unique union of media library IDs through actual HongGuo bindings for those works. No bound media means no document; incremental refresh deletes its former document. Pending/unbound files do not make a work searchable.
4. The title remains the earliest stored official season's title even if only later seasons have files. Membership and display ownership are separate. Do not index episode titles, file paths or user-specific flags.
5. Backfill continues to advance by the candidate identity cursor, not emitted document count: a batch containing only fileless works must not stop later playable works from being indexed. Preserve bounded batches and dirty replay.

## Search path

`request visibility -> positive library filter -> HongGuo OpenSearch -> bounded current-DB verification -> existing three-source merge/page/card loading`

- For ordinary Web keyword/suggestion searches, no global HongGuo `CandidateIDs` list is constructed. Reuse existing filter-preparation behavior, including explicit/hidden-only scopes and locked-empty scope handling. A multi-library document is eligible if at least one library is currently allowed; never reject it simply because another membership is hidden.
- Keep title-only OpenSearch recall and the current 100-candidate limit. OpenSearch library filtering occurs before that limit. The final database query resolves only returned identities, checks current file/library visibility and the existing title-substring rule, and returns existing ranking fields.
- Successful zero hits return immediately; backend error/incompatibility is not zero hits. PostgreSQL fallback retains its pre-limit scope and matching semantics.
- Person/favorite-qualified HongGuo searches retain their existing pre-limit scoped CandidateIDs path, since those user/credit predicates are not new index fields. Played/unplayed/resumable/hierarchy paths stay in their existing owners. No new Emby routing is introduced.
- Keep request cancellation, error propagation, three-source joins and current ordinary/NFO-only versus HongGuo-merged page caps unchanged.

Normal-search library normalization is explicit:

| Incoming scope | Index filter before limit | Final database scope |
| --- | --- | --- |
| Locked/restricted with empty `AllowedLibraryIDs` | Return empty without index or candidate SQL, preserving the current HongGuo guard | No query |
| Nonempty allowed libraries | Positive `VisibleLibraryIDs = unique(allowed) - hidden`; empty result returns immediately | Apply the same allowed and hidden predicates through `hongGuoFileScope` |
| No allowed restriction, nonempty hidden libraries | Reuse the current preparation query for distinct media library IDs outside hidden; pass as a positive restricted set | Apply the same hidden predicate; never substitute a document-level `must_not` over all memberships |
| Neither allowed nor hidden restriction | Unrestricted library filter; documents themselves must be file-backed | Require a current bound HongGuo media row |

Apply the existing locked-empty guard before deriving the normalized copy; reset derived `VisibleLibraryIDs`/preparation state from the raw allowed/hidden inputs so a raw restricted filter is not mistaken for an already prepared one. Cover allowed-only, hidden-only, intersection-empty and mixed visible/hidden documents with more than 100 competing ineligible hits. The hit verifier receives the same request kinds and raw media-scope constraints, not an independently broader visibility default.

## Membership synchronization

Use one shared configured `HongGuoRepository` for document refresh/failure/dirty state. Wire the existing Media repository to that instance for its owned writes; do not construct a fresh coordinator on each call.

Add a small HongGuo-specific capture/refresh helper reused by the existing mutation owners in `research/findings.md`. The helper captures old bound source work IDs/logical identities for the caller's exact media scope, and recomputes affected old/current identities after commit. Keep ordinary metadata notifications and each caller's transaction, batching, filesystem and error behavior intact; no general media-delete refactor is required.

- Add/upsert/rebind: capture the old binding before replacement and include the new work after commit. Moving a file between libraries updates the union; replacing the last valid binding with pending removes the identity.
- Partial rebind/bulk-delete progress: committed changes must still be refreshed if a later item fails or cancellation occurs. Deduplicate/batch affected identities rather than perform a whole-library reindex per file. An index refresh using a cancelled context must mark failure; never silently treat it as synchronized.
- Delete/library/root/duplicate/organizer paths: capture before bindings disappear, notify after successful database commit. Preserve the library deletion's outer transaction. Index HTTP must not run inside a database transaction.
- Detail/album/catalog cleanup: retain existing post-commit refresh of old/new identities and title owners; membership is recomputed by the common projector.
- Reuse serialized HongGuo incremental writes and rebuild dirty replay. If capture/recomputation/index publication fails after a committed change, mark the shared HongGuo index unreliable so search falls back; do not report a database rollback that did not happen. A capture failure must not silently enable stale indexed reads.

Rollback and ownership assertions are mandatory: a rolled-back mutation publishes no document upsert/delete and contributes no committed dirty IDs; for independently committed batches, only committed batches are notified even if the next batch rolls back. A cancellation after commit must not skip invalidation merely because the return value is an error. Service-owned writes use `s.repo.HongGuo`/the corresponding shared container instance; Media-owned writes use its wired pointer to that same instance. A synchronization-failure regression must show that a failure through a deletion/upsert owner is observed by the search reader and rebuild owner, not by an isolated temporary repository.

## Compatibility, recovery and consistency

- Use HongGuo schema version 2 for the new document contract; ordinary metadata remains version 1. Readiness, generated index names and orphan cleanup use the backend's expected version. Existing HongGuo version 1 is not a valid fast-path index. Keep aliases and document identity stable.
- Reuse startup warmup and existing settings/delay/concurrent sources; do not force an ordinary schema migration or alter its warmup behavior. A configured HongGuo runtime starts conservatively on PostgreSQL until its first successful reconstruction, so a previous process's interrupted post-commit write is not trusted after restart. If warmup is disabled/fails, search remains functional on PostgreSQL but does not gain the indexed speedup.
- Rebuild into a separate index, replay committed changes and refresh before atomic alias activation. Failed/cancelled rebuilds do not activate incomplete targets. After this process has completed a successful HongGuo reconstruction, its trustworthy compatible alias can remain usable during a later routine HongGuo rebuild unless marked unreliable by a failed update. This never applies across process restart: even a persisted version-2 alias must not bypass the new process's initial reconstruction gate. Ordinary-source rebuilds do not change HongGuo readiness.
- Incremental search visibility retains OpenSearch's near-real-time refresh boundary, not a new linearizable read-after-write guarantee. File membership changes can take a short refresh interval to appear; current-database verification must never expose newly forbidden files. Tests distinguish index acceptance from search refresh. Do not add `refresh=wait_for` to every scanned episode or invent a durable distributed queue silently.
- Existing in-process serialization is not multi-process writer coordination. Out-of-band SQL and multiple writers remain outside this task. This limitation and the startup fallback must be stated in final handoff.
- Rollback is code rollback plus PostgreSQL fallback/rebuilding the compatible HongGuo index. Version 2 is rejected by old version-1 readers; there is no PostgreSQL data migration. No live rebuild, deployment, restart or alias mutation is performed without a separate user request.

## Verification and expected files

Primary files: `repository/hongguo_search_index.go` (projection/search), `opensearch.go` (per-backend version), `repository.go` / `media_repository.go` / `hongguo_media.go` (shared instance and mutation notifications), existing service mutation owners listed in the research matrix, and directly relevant tests. Reuse existing coordinator internals; modify them only for a demonstrated missing failure/replay guarantee, without changing ordinary behavior. Spec updates belong to the existing HongGuo search scenario after verification.

Tests must cover permission filtering before limits, mixed hidden/visible membership, locked/empty scopes, fileless and later-season-only documents, empty backfill batches, bounded revalidation plans, production mutation entry points, rollback/partial progress/cancellation, old-index rejection, startup and incremental failure fallback, dirty replay and unchanged three-source results/caps. Independent review follows implementation. Stable-snapshot functional equivalence is separate from performance under external load; no fixed millisecond promise is made.
