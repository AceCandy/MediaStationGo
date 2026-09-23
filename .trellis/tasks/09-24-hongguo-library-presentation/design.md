# Design

Use the existing library series and file-detail routes. Extend their read projections for HongGuo alongside the existing NFO adapters, without writing canonical metadata surrogates. Remove the dedicated HongGuo library rendering branch.

Repository changes own library-scoped logical pagination, distinct episode counts, season representative files, and batched whole-series presentation. Official album membership selects the earliest stored season for every whole-series field; seasons retain their own work metadata. Public hg-* identities and source-ID state remain unchanged. No schema migration.

Service/handler adapters supply source credits, episode versions and independent user state through existing Web response shapes. Reuse existing mixed-source Continuations for cross-season playback. Preserve bounded list loading and visibility checks before hydration. Legacy hongguo_id resolves to the official logical series within the requested library.

Frontend reuses MediaCard and the existing library series detail hierarchy, with source-aware management controls. Missing episode fields pass through unchanged to existing placeholders. Movies retain the file-detail path. No new component framework or new dependency.

Changes are confined to these repository/service/handler projection seams, library routing/state, shared detail controls, regression checks, and the owning spec. Existing source data can roll back with the code because storage contracts do not change.
