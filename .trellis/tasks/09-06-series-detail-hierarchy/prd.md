# Series detail hierarchy

Approved follow-up: expose local Season/Episode metadata correction as a task
with progress, daily logs and manual retry. Automatic correction runs once per
rules version after success, independently of ingestion workers. Never overwrite
Web edits, including edits made after the task reads a row. No network fetching,
deployment or production task execution is authorized for implementation tests.

User approved implementation of the previously researched design.

Episode follow-up: reuse movie track-row visuals while retaining read-only
track information; subtitles use the compact movie chooser for viewing only.
Keep media information adjacent to the still with long
synopsis content. Hide Episode ratings and Douban association, retaining TMDb.

Follow-up: remove whole-series smart scraping and organizing from the Series
detail menu. The user also approved removing organizing from movie and Episode
details, including their unused dialog/state. Keep dedicated organizing tools
and backend endpoints unchanged.

- Reuse the movie artwork and metadata language for Series-owned information.
- Select seasons and logical episodes in place; explicit playback targets the selected concrete version.
- Restore season, episode and version through URL state, including deep links outside the first library page.
- Keep versions/tracks/files scoped to one episode and whole-series management scoped to all files.
- Show Series-owned favorite and credits; never substitute Episode metadata for Series metadata.
- Handle specials, long seasons, loading, missing metadata and stale asynchronous responses.
- No dependencies, player track-selection feature, filesystem behavior changes or unrelated refactors.
- Series and Episode synopsis, file facts and read-only tracks are directly visible without folding panels; keep season/episode/version selection and movie defaults unchanged.
- Keep Series as the main hero, integrate a small canonical Season cover/title with episode selection, and present the selected Episode with movie-like desktop columns and mobile reading order. Missing Season metadata uses season numbers/counts and a cover placeholder, never Series/Episode artwork.

Verification: focused Go tests, runnable selection regression check, Web lint/build, independent diff review and responsive browser checks where available.

## Approved follow-up: long-series ingestion

- Prefer library ingestion over catalog completion at season/episode boundaries.
- Show waiting task executions before lock acquisition.
- Bind each claimed file to its own Episode, sharing only same-episode versions.
- Reuse Season inventory for basic display fields without claiming full hydration;
  refresh snapshots missing newly requested episodes and retain durable completion.
- Keep three ingestion workers, existing write protection and current queues.
- Verify using isolated PostgreSQL schemas; do not repair production data or
  deploy/restart services automatically.
