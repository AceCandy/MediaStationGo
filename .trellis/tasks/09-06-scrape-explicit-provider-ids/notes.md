# Implementation and validation

- Removed automatic provider title/year searches in regular enrichment and pre-organize lookup, plus their candidate selection and name-cache helpers.
- Preserved explicit provider IDs, manual search/apply and local NFO behavior. TMDb 404 is no-match; returned provider IDs must equal requested IDs.
- Independent review identified an existing-DB-ID omission in reclassification; added direct-ID fallback and regression coverage.
- `go test ./... -run '^$'` passed (compilation check, not a full test run).
- PostgreSQL-backed focused regression passed: ID-only requests, no-ID no-match, explicit second-provider fallback, mismatched returned IDs, organize classification/deduplication, library enrichment, manual search, NFO boundaries and existing DB IDs for reclassification.
- Expanded scraper/organizer regression reports 29 failures. All 29 also fail on unchanged commit `15f9aa7` in a separate baseline worktree; no additional failing test names after updating fixtures for explicit IDs. Existing failures include missing `media_probe_metadata` in test schemas and existing organize/metadata expectations. They were not repaired in this task.
- No production database, real provider services or Emby clients were tested. No historical metadata was rematched.
- Temporary PostgreSQL container, baseline worktree and test logs were used only for verification and removed afterward.
