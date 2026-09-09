# Verification

## Implemented

- People: SQL Chinese/negative-cache filtering before a bounded first page; only ID/source and selected-person work contexts loaded.
- Roles: SQL filtering, `(metadata_id,id)` keyset pages of 100, page-local metadata fields; preserve first 1,000 groups and all selected-group targets across later pages.
- Douban: unique snapshot LEFT JOIN and non-inlined empty-path JSON extraction to reuse detoasted payload across predicates.

## Checks

- Isolated PostgreSQL 17 container, test-only database, no production writes.
- Passed repository/service targeted tests: PendingPeopleTranslations, ScheduledPeopleTranslation, PendingRoleTranslations, PeopleTranslationEmptyPass, TranslatePeopleWindow, ListDoubanMovieEnrichmentAfter, DoubanMovieEnrichment, DoubanEnrichmentCandidate.
- Fixed three stale fixtures/assertions in the targeted tests: ambiguous Douban ID attached to wrong movie, missing MetadataCredit migration, and missing existing success marker in expected task detail.
- Independent read-only review: no mandatory semantic regressions in implementation files. Verified unique snapshot and non-null cache constraints directly in models and snapshot uniqueness regression test.
- gofmt and git diff --check passed. No full repository or frontend suite run for this backend-only scope.

## Read-only DBX Measurements

- People old database execution: 125.961 ms, 54,869 result rows. New: 64.043 ms, no currently eligible rows. Original application logs include additional result processing and are not directly comparable to EXPLAIN timings.
- Roles old database execution: 437.744 ms, 46,423 result rows. New: 564.906 ms, no currently eligible rows. Payload transfer/preload is eliminated for idle sweeps, but table scanning remains and SQL execution alone has not improved.
- Douban alternating runs (ms): old 1265.895 / 1268.392 / 1293.469; new 512.046 / 538.372 / 516.656. Median 1268.392 -> 516.656, about 59% reduction.
- Fixed-cutoff old/new Douban result symmetric difference: 0 on current data. JSON and eligible cases additionally covered by isolated regression fixtures.

## Remaining Boundary

User approved a role partial-index startup migration. `ensurePerformanceIndexes` now creates the `(metadata_id,id)` index for nonempty unchanged non-Chinese roles; bound credit types are deliberately excluded from the predicate. No production DDL executed. No user service restarted or code committed. The first isolated test container was stopped and automatically removed, including its disposable test data.

## Index Follow-up

- PostgreSQL 17 isolated test with 30,000 credits and 100 pending roles: forced generic prepared plan used `idx_metadata_credits_pending_translation`, 100 rows, 3 buffer hits, 0.074 ms execution (synthetic fixture, not production end-to-end timing).
- Repeated migration passed; candidate results remain 100. Initial startup index creation can delay startup and block concurrent credit writes; production timing still requires user restart and observation.
- Independent index review found no issues in predicate, keyset order, migration order or generic-plan test. Expanded regression also encountered the previously confirmed baseline failure TestMetadataSearchCountsPlayableTopLevelWorks; no unrelated search changes made in this follow-up.
- Final targeted database/repository/service regression passed; git diff --check passed. The index test container was stopped and automatically removed. No production index build or full-suite verification was performed.

## Type-leading Index Correction

- Production read-only check after first restart: old partial index valid, 11 MB; full role query 154/110/109 ms, still discarding 112,025 Writer/Director entries to obtain 1,111 Actor/GuestStar entries.
- User approved `(type,metadata_id,id)`. Startup now creates `idx_metadata_credits_type_pending_translation` before dropping the exact obsolete index; existing query and predicate unchanged.
- Regression fixture now contains 29,900 non-Chinese Writer/Director entries plus 50 Actor and 50 GuestStar entries. Generic prepared plan uses type as an index condition, returns only 100 rows and has no Rows Removed by Filter. This closes the earlier fixture gap where all irrelevant rows were Chinese and excluded by the partial predicate.
- Tests cover obsolete-index replacement and repeated migration. Production timings for this replacement are not measured until the next user restart. No production writes or service restarts performed.
- Final database and translation service targeted tests passed; separately reviewed create-before-drop order and exact obsolete name, and git diff --check passed. Disposable type-index test container stopped and removed. No full suite run.
