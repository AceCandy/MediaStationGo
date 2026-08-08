# Implementation Plan: Persist Emby People Images

1. Add `Person.ProfileImageKey` and extend credit inputs/upsert updates; verify
   SQLite `AutoMigrate` and provider-ID reuse preserve the key on failed
   refreshes.
2. Extract the shared image content validation/hash/sharded-path/atomic-write
   helper from `ArtworkStore`; keep existing artwork paths and behavior
   unchanged, then add `PeopleImageStore` rooted at `DataDir/people`.
3. Wire the store into the service container and scraper. Persist successful
   profile downloads directly to `DataDir/people` before credit DB
   transactions, bypassing `cache/images` and its failure markers.
4. Update the Emby image handler to serve person profile keys from the people
   root before any generic proxy path; preserve placeholder, `HEAD`, prefix,
   casing, and non-person behavior.
5. Add focused tests for storage layout/deduplication, direct remote import
   without cache artifacts, failed-refresh key preservation, and Emby person
   image responses without request-time network access.
6. Replace cloud metadata artwork cache import with direct cloud resolve and
   download into `DataDir/artwork`; verify poster and backdrop persistence,
   local artwork URLs, required direct-link headers, and no cache artifacts.

Validation commands:

- `gofmt` on changed Go files.
- Focused `go test ./internal/service ./internal/repository ./internal/handler`.
- Full `go test ./...` after focused tests pass.
- `git diff --check` and a final read-only review of all changed paths.

Rollback points:

- Before service wiring: model/repository changes are isolated and
  auto-migratable.
- Before Emby routing: storage tests must pass independently.
- Before direct remote import wiring: local-file import and serving remain
  independently testable.
