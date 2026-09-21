# Verification

- Real isolated PostgreSQL: service/repository/handler playback, continuation,
  NextUp aliases, target-user isolation, NFO, HongGuo, hierarchy, previous-episode
  marking and multipart regression selection passed (29.956s/5.827s/7.347s).
- Added three-source unknown-duration, hidden replacement, snapshot preservation,
  other-user and event-count assertions; focused test passed (3.562s).
- Final write-side guard requires a known non-multipart replacement duration;
  playback-state, cross-season and multipart regression rerun passed (8.314s).
- Web lint/build, series selection, history presentation and browser NextUp/catalog
  checks passed. Browser checks use mocked APIs, three viewports, both themes,
  accessibility, search/filter/copy and non-admin access.
- Independent read-only review completed. Claimed hydration index panic was
  rejected after tracing hydrateHistory: it returns exactly one entry per input
  or nil on error. Actual PostgreSQL tests verify correlated LATERAL SQL and plans.
- Multipart fixture lacked the production partial unique history index and event
  table; added both locally to the affected test only.
- Unrelated TestEmbyMediaSourceUsesLocalSTRMTargetContainer fails its legacy
  bitrate expectation. Its mediaSource implementation and fixture are unchanged;
  no bitrate behavior was altered in this task.

## Unverified / limitations

- No deployment, production migration, production data rewrite or real Emby
  client end-to-end validation. No full-suite green claim.
- Reconciliation requires a known non-multipart replacement probe duration;
  unknown duration and multipart do not infer completion.
- Different edits of the same episode can differ semantically; user accepted
  duration-based cross-version inference.
- Read projection also handles rebound/hidden source references, while automatic
  write carry-over specifically protects physically deleted sources. Persisting
  derived completion across rebinding/permission changes is not guaranteed.
- Existing uncommitted changes remain intact. finish-work archival is deferred
  because its prerequisite code commit has not been requested/performed.
