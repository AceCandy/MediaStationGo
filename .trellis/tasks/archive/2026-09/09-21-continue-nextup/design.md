# Shared continuation selection

Use narrow per-source SQL projections in the history repository. Select visible user states before grouping; prefer the latest resumable episode, otherwise select the furthest completed season/work/episode coordinate. NFO and HongGuo batch marks write individual timestamps, so completion boundaries must not depend on write order. Group recency is the maximum watched timestamp. A scoped lateral query finds the first later visible unplayed episode. HongGuo ordering is season_index, source_id, episode number; state remains source_id/episode number. Natural ordering never sends positive-season progress back to specials.

Merge source candidates before final pagination, hydrate only selected files/items. Exact Emby counts count eligible candidates, not raw history. Web receives a read-only recommendation marker and zero progress, never persisted history. Emby NextUp uses existing batched payload generation and authenticated target-user handling. Resume and IsResumable stay unchanged.

Risks: equal timestamps from bulk marking, same-season HongGuo members, exhausted groups ahead of eligible groups, visibility before limit, and PostgreSQL planners expanding unrelated catalog rows. Verify each with executable regressions. No schema/index change until plans show a need.
