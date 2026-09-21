# Implementation and validation

1. Add shared source-scoped continuation queries and regression fixtures.
2. Connect Web ContinueHistory and Emby NextUp, all route aliases and documented parameters. Preserve recent/full history and existing Resume paths.
3. Mark next-episode Web cards distinctly, synchronize the Emby API catalog.
4. Run focused PostgreSQL tests including plan rows/loops, route/user isolation tests, Web lint/build and relevant presentation checks.
5. Independently review the diff and source contracts, fix findings, update playback contract. Preserve pre-existing unrelated changes. Do not start persistent services or commit unrelated work.

## Verification result

- Focused repository/service/handler PostgreSQL regressions passed on an isolated temporary PostgreSQL 15 UTF8 database, including existing Resume, NFO, hierarchy and permission tests.
- Plan guards passed with 2,000 series and 4,000 episodes per source plus other-user states. Per-source selection measured approximately 0.1–0.3 ms on that fixture; this is not a production or full-endpoint latency claim.
- Web lint, production build, history-presentation checks and browser nextup checks passed. Browser checks used mocked APIs, all three viewport sizes, both themes, scoped accessibility, filtering, copy success/failure and non-admin redirection.
- Independent review found the old series-only card link did not target the recommended episode. Fixed next cards to link the concrete file and tested the real redirection contract. The proposed extra legacy-source predicate was unnecessary: the existing media CHECK prevents independent catalogs from carrying canonical metadata IDs.
- Real player integration, production database load and deployment remain external acceptance. Product changes are uncommitted; unrelated pre-existing changes were preserved.
