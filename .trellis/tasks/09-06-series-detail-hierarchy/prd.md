# Series detail hierarchy

User approved implementation of the previously researched design.

- Reuse the movie artwork and metadata language for Series-owned information.
- Select seasons and logical episodes in place; explicit playback targets the selected concrete version.
- Restore season, episode and version through URL state, including deep links outside the first library page.
- Keep versions/tracks/files scoped to one episode and whole-series management scoped to all files.
- Show Series-owned favorite and credits; never substitute Episode metadata for Series metadata.
- Handle specials, long seasons, loading, missing metadata and stale asynchronous responses.
- No dependencies, player track-selection feature, filesystem behavior changes or unrelated refactors.

Verification: focused Go tests, runnable selection regression check, Web lint/build, independent diff review and responsive browser checks where available.
