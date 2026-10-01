# Episode track backfill on detail access

## Goal
Opening an episode detail through Web or Emby, or resolving Emby PlaybackInfo, pre-fills missing track documents for every visible file of the same-season episode number +1, including HongGuo.

## Requirements
- Successful Emby PlaybackInfo requests also wake the same scope for continuous playback without detail access; rejected selections do not enqueue work.
- Resolve canonical season identity and exact episode number +1; do not cross seasons or skip missing episodes.
- Include every version visible to the requesting user; skip current valid probe documents.
- Reuse the existing TaskKindProbe coordinator, task history, logs and mutual exclusion.
- Run the requested files serially with a one-second gap between attempts; retain existing remote throttling.
- Detail requests do not wait for probing and do not schedule an entire series. Detail-triggered work never deletes damaged media.
- Preserve startup/ingestion exclusions for episodic media. Remove the obsolete Web-only single-version trigger.

## Acceptance
- Actual Web/Emby episode detail access and successful Emby PlaybackInfo schedule exact next-episode files across ordinary, NFO and HongGuo identities.
- Tests cover multi-version scope, visibility, gaps, existing documents, queue coalescing, active-task waiting, spacing and cancellation.
- Targeted PostgreSQL tests, Web lint/build, and diff checks pass; independent review confirms detail and playback entry points.
