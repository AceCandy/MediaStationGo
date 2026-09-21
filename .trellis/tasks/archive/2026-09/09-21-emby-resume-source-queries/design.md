# Design

Replace global resumable node expansion with lightweight per-source queries driven by user states. Canonical history is unique per active user/metadata, NFO per user/item, HongGuo per user/source/episode. Apply existing visibility, kind, search, favorite and person rules before selecting the newest episode for each resume key.

Count each filtered source's distinct resume keys independently. Bound each grouped source to offset + limit, then UNION ALL only those bounded rows for PostgreSQL ordering and final pagination. Keeping the merge in PostgreSQL preserves database text collation and NULL ordering for all supported sorts. No shared cross-catalog aggregation is required.

Reuse the existing mixed-page payload loader, extracted without behavior changes. Keep no-NFO ResumeItems on its existing path to preserve its historical progress threshold and version selection. No public API or schema change; rollback is a code revert.

Files: service/emby_resume.go (candidate selection), service/emby_hongguo_browse.go (routing and shared payload loader), service/emby_resume_test.go (behavior/plan regression), playback spec (query contract).
