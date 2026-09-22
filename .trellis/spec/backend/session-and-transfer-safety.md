# Session and Transfer Safety

## 1. Scope / Trigger

Applies to token issuance, account/password updates, browser session replacement,
organizer transfers, scheduler lifecycle, and database CI.

## 2. Signatures

- `users.token_version` and `refresh_tokens.token_version`: non-null bigint,
  default 0, omitted from public JSON. JWT claim `ver` carries the same snapshot.
- `TokenService.IssuePair(ctx, *model.User)` signs the captured user version.
- `UserRepository.UpdatePassword(ctx, id, hash)` updates password, increments
  version, and revokes refresh rows in one transaction.
- `renameNoReplace(src, dst)` uses Linux `RENAME_NOREPLACE`; EXDEV selects
  exclusive-copy fallback. Other platforms always select that safe fallback.
- `SchedulerService.Stop()` cancels and joins all admitted jobs and timer loops.
- `AuthState.sessionVersion` is in-memory only; setSession/logout increments it.

## 3. Contracts

- Active-user guards compare JWT version with the current database row before
  granting access; role/tier come from that row, never stale JWT privileges.
  Admin and API-config routes run the guard before `AdminRequired`.
- Missing historical JWT versions are 0. Existing sessions survive migration,
  but password change/reset invalidates Web, refresh, Emby and scoped playback
  tokens. Every signer, including default STRM generation, must carry version.
- A refresh row inserted after password change from an old user snapshot remains
  invalid. Do not reload the current version to legitimize that old snapshot.
- `/api/auth/refresh` returns tokens but does not set the browser access Cookie;
  current-session Bearer requests synchronize the image Cookie as before.
- Axios requests capture sessionVersion. Old responses/retries are canceled;
  refresh promises are shared only within one version and retry at most once.
- Library API calls with AbortSignal bypass shared-request deduplication;
  effect cleanup aborts requests, invalidates pagination, and ignores stale results.
- A failed organizer database update compensates its transfer. Check destination
  file identity before compensation and never overwrite a newly occupied source.
  A changed/missing destination is reported, not deleted. This is best-effort
  compensation, not a transaction against crashes or arbitrary external writers.
- Scheduler admission and WaitGroup.Add share the shutdown mutex. Manual jobs
  retain caller values without HTTP cancellation but observe scheduler cancellation.
  Stop the scheduler before releasing its downstream services.
- CI sets `MEDIASTATION_TEST_POSTGRES_DSN` using PostgreSQL 16 and isolated schemas.
  Missing local DSN still skips database tests; that is not full validation.
- Database regression fixtures must model the actual persistence boundary:
  `Create(&[]*T{&first, &second})` writes generated IDs back to the originals;
  a temporary value slice does not. Compare reloaded timestamps or truncate to
  PostgreSQL microseconds, and compare JSONB structurally rather than as text.
- `newUnboundTestScraper` disables legacy auto-created local metadata so direct
  provider tests cannot pass through canonical reuse without querying the provider.
  Supply explicit provider IDs and valid Season inventories; never restore
  automatic title matching to satisfy an old test. Probe fixtures require a full
  versioned document, and progress fixtures require the production partial indexes.

## 4. Validation & Error Matrix

- Missing/deleted/revoked account -> 401; disabled/expired account -> 403.
- Current non-admin role -> management routes 403, ordinary routes retain access.
- Password transaction failure -> unchanged hash/version/refresh state.
- Existing file, empty directory or dangling destination link -> no overwrite.
- Compensation source collision/destination replacement -> preserve files, report
  both the original database error and restoration failure.
- Run admitted after scheduler shutdown -> `ErrSchedulerJobNotFound`.

## 5. Good / Base / Bad Cases

- Good: same-user re-login isolates older refresh completion; a new refresh may
  proceed without waiting for the previous session.
- Base: same-filesystem move uses one no-replace rename; cross-device move copies
  exclusively and deletes source only after a successful close.
- Bad: `Stat(dst)` followed by ordinary `Rename`, deleting a replaced destination,
  or using `WithoutCancel` without a service-lifetime cancellation source.

## 6. Tests Required

- `TestAuthenticatedRoutesUseCurrentAccount`,
  `TestPasswordChangeRevokesSessionsAtomically`,
  `TestRefreshDoesNotOverwriteBrowserCookie` and STRM token version assertions.
- `TestConcurrentMovesNeverOverwrite`, `TestTransferCrossDevice`,
  `TestOrganizeDatabaseFailureRestoresTransfer`, `TestReclassificationDatabaseFailureRestoresFile`
  and `TestRollbackTransfer*` with injected database failure and destination replacement.
- `TestScheduler*` and `TestHongGuoSupplementStopCancelsAndJoins` under `-race`.
- `node scripts/check-auth-session.mjs`, `node scripts/check-series-loading.mjs`,
  frontend lint/build, and real PostgreSQL integration tests.

## 7. Wrong vs Correct

Wrong: refresh success blindly replaces auth state; privileged routes trust JWT
role; Stop waits for only one special job kind.

Correct: compare the request generation before any auth mutation/retry, resolve
current authorization centrally, and cancel/join every admitted scheduler job.
