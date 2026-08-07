# Implementation Plan

## Phase 1: Models and Repository

- [x] Add `Person`, `PersonIdentifier` and `MetadataCredit` models, active unique indexes and `AllModels()` registration.
- [x] Add repository normalization, person upsert, credit snapshot replacement, list/search/detail queries.
- [x] Extend metadata graph merge to migrate and deduplicate credits before source deletion.
- [x] Update focused database/repository test migration helpers.

Validation:

```bash
go test ./internal/database ./internal/repository -run 'Person|Credit|Metadata.*Merge|Schema'
```

Rollback point: model constraints and replace/merge transaction tests pass before scraper changes.

## Phase 2: TMDb and NFO Collection

- [x] Add provider-neutral `PersonCredit` and explicit loaded credit types to `Match`, episode details and `LocalMetadata`.
- [x] Parse TMDb movie/TV cast plus Director/Writer crew, and Episode guest stars plus Director/Writer crew.
- [x] Preserve NFO actor/director/writer/credits structurally.
- [x] Merge local NFO only into TMDb-empty credit types; never union name-matched rows into non-empty TMDb types.
- [x] Persist movie/Series and Episode snapshots without copying inherited rows.
- [ ] Verify an explicitly loaded empty type removes stale credits while providers without that type contract leave existing credits unchanged.

Validation:

```bash
go test ./internal/service -run 'TMDb.*(Movie|TV|Episode|Credit|Cast|Crew)|LocalMetadata.*(Actor|Director|Writer)|Scrape.*Credit'
```

Rollback point: provider and local persistence tests pass before exposing People through Emby.

## Phase 3: Emby People and Person Endpoints

- [x] Populate Movie, Series and Episode `People`, with per-type Episode-to-Series inheritance.
- [x] Implement `Persons` service query and uppercase/lowercase handlers.
- [x] Support `IncludeItemTypes=Person`, person ID filtering and person item detail.
- [x] Resolve person profile URLs through the existing item image handler.
- [ ] Keep unsupported mixed Person/media item queries behavior explicit and tested.

Validation:

```bash
go test ./internal/service ./internal/handler -run 'Emby.*(People|Person|Image|Item)'
```

## Phase 4: Backfill Path and Regression Check

- [x] Add `AIService` structured batch translation for missing people/role display values.
- [x] Add `metadata.people_ai_translate` runtime setting and SettingsPage toggle, default off.
- [ ] Verify disabled/unconfigured/invalid/error AI paths preserve originals and do not fail scrape; unchanged translations are not requested again.

- [ ] Add a regression test proving `IncludeMatched=true` re-scrape replaces credits for an existing matched metadata item.
- [ ] Confirm no new admin endpoint or implicit all-library refresh was introduced.
- [ ] Run formatting, focused tests, full Go tests and static diff checks.

Validation:

```bash
gofmt -w <changed-go-files>
go test ./internal/database ./internal/repository ./internal/service ./internal/handler
npm --prefix web run build
npm --prefix web run lint
go test ./...
git diff --check
```

## Independent Review Gate

- [ ] Trace TMDb/NFO -> repository -> metadata merge -> Emby item/person/image end to end.
- [ ] Verify soft-delete restoration and unique indexes on SQLite; statically inspect PostgreSQL compatibility.
- [ ] Verify no name-only merge crosses local and TMDb sources.
- [ ] Verify only Actor/GuestStar/Director/Writer entered the diff and no subscription or Douban people fallback was introduced.
- [ ] Verify frontend changes are limited to AI settings/API configuration controls and no media-detail UI entered the diff.
- [ ] Verify no temporary, cache, credential or local debug files were created.
- [ ] If any service is started for manual verification, stop it before completion.

## Phase 5: Admin AI Configuration and Web Search

- [x] Add model and web-search fields to the existing API config persistence/public/resolve/patch contracts.
- [x] Let database-backed model configuration override the file fallback.
- [x] Add OpenAI-only model and web-search controls to the active admin API panel.
- [x] Route web-enabled AI chat through Responses API with the hosted `web_search` tool; route People translation through Responses API without tools.
- [x] Add focused round-trip and HTTP request/response tests, then run frontend lint/build and static diff checks.

Validation:

```bash
go test ./internal/service -run 'APIConfig|AI.*(Database|Responses|WebSearch|Translate)'
npm --prefix web run build
npm --prefix web run lint
git diff --check
```

## Phase 6: Context-Aware Asynchronous People Translation

- [x] Add a context-scoped translation cache model and repository lookups/upserts.
- [x] Remove synchronous AI work from credit persistence and signal a service-lifetime worker after the snapshot commits.
- [x] Discover pending people/roles on startup and periodically so interrupted work resumes after restart.
- [x] Add up to three associated works for person names and exact work context for roles.
- [x] Resolve cache hits first, deduplicate misses and split Responses requests by 100 entries plus a character cap.
- [x] Persist only valid Chinese translations and conditionally update unchanged Person/Credit rows.
- [x] Cover cache isolation/hits, context payloads, batch splitting, asynchronous persistence and stale-write rejection.

Validation:

```bash
go test ./internal/repository -run 'TranslationCache|Person|Credit'
go test ./internal/service -run 'PeopleTranslation|TranslatePeople'
git diff --check
```

## Phase 7: Long Role Compatibility

- [x] Store credit source/display roles and translation cache source/display values as text.
- [x] Explicitly upgrade existing PostgreSQL columns from `varchar(255)` to `text`.
- [x] Cover a role longer than 255 characters and assert PostgreSQL schema types.

Validation:

```bash
go test ./internal/repository -run 'LongRole|PostgresTextColumns|TranslationCache'
go test ./internal/database
git diff --check
```

## Known Risks

- Different Emby clients may request additional Person field variants; tests cover repository-observed route/query patterns, but real-client interoperability remains a manual verification item.
- NFO-only same-name people can still collide within the local source because NFO has no stable identifier; avoiding all duplicates without an external ID would require user-assisted identity management.
- Profile images remain dependent on the remote TMDb URL after proxy cache eviction.
- PostgreSQL integration is only run when a configured test DSN exists; otherwise compatibility is statically reviewed and reported.
