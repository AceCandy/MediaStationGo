# Design

GET and POST /Users/:userId/Items/:id/PlaybackProgress are authenticated MediaStationGo extensions. GET returns effective PositionTicks, RunTimeTicks, Played, concrete MediaSourceId and Revision (decimal string). POST requires those fields and ExpectedRevision; stale writes return 409. All state remains per logical catalog identity and user.

A dedicated revision table retains deletion tombstones. AFTER row triggers on the four state tables increment revisions for playback-field or identity changes, including existing writers, soft/physical deletion and merge. NFO favorite-only changes are excluded. No existing-row backfill is needed: existing states begin at revision zero and the next mutation advances them.

CAS locks the actual state tuple before reading its revision, preserving the existing writer lock order (state then revision). When absent, INSERT ON CONFLICT DO NOTHING creates a provisional zero row, then locks the actual tuple. Its own insert increment is accounted for; a mismatch rolls the entire transaction back, including the provisional row and revision. Exact writes update explicitly, avoiding sticky automatic completion and thresholds. Read uses a repeatable-read snapshot for effective-state projection and revision. No statistics events or supplemental previous-episode marks.

Media visibility and selected concrete-source membership are checked before access. Deleted sources are rejected, never substituted. Duration/probe compatibility is checked for explicit sync. Reads retain the shared effective-state projection. Response verification and pending conflict policy belong to LinPlayer.
