# Persist TMDb discover catalog metadata

## Goal

When a user browses the TMDb-backed discover page, retain the fetched catalog
entry as canonical metadata without delaying the page response. Persisted
catalog metadata must be reusable when a real media file is scanned later.

## Requirements

- Queue only TMDb discover entries with a valid TMDb ID for asynchronous
  persistence; the discover HTTP response must not wait for the work.
- Fetch the complete TMDb movie or TV match, then persist the canonical
  `MetadataItem` and its `tmdb` identifier.
- Persist the complete provider credit snapshot, including people and profile
  images, through the existing shared people/credit path.
- Persist poster and backdrop bytes under `DataDir/artwork/sha256/...` and
  person profile bytes under `DataDir/people/sha256/...`.
- Keep catalog metadata permanently; do not create a fake `Media` row or an
  `is_in_library` flag. Later scanner resolution must bind by the existing
  TMDb identifier contract.
- Deduplicate queued work by TMDb ID and media kind. Failed work must remain
  retryable when the entry is observed again; successful work must not be
  rehydrated on every render.
- Stop and join the worker with the service lifecycle. Existing unrelated
  working-tree changes must remain untouched.

## Acceptance Criteria

- [ ] `/discover/feed` returns section results without waiting for TMDb detail,
      credit, or image persistence requests.
- [ ] A successful queued movie/TV item creates or reuses its canonical
      metadata and `tmdb` identifier, with a completion timestamp.
- [ ] A successful item stores artwork and credit profile images in the two
      `DataDir` stores and creates no `Media` row.
- [ ] Repeated entries in one or many sections result in one queued hydration;
      completed entries are skipped, while failed entries can retry.
- [ ] Scanning a media row carrying the same TMDb ID resolves the persisted
      metadata through the existing repository path.
- [ ] Worker cancellation is clean and existing focused tests plus
      `git diff --check` pass.
