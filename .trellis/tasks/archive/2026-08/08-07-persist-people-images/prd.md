# Persist Emby people images locally

## Goal

Persist every available person profile image and selected metadata artwork
under the application's data directory so Emby clients receive managed local
images independently of the disposable remote-image cache.

## Background

- `Person` currently stores only the source `ProfileURL`; people are not
  `MetadataItem` artwork and must not be attached to `MetadataArtwork`.
- Movie and series artwork is stored under `App.DataDir/artwork/sha256/...`
  through `ArtworkStore`.
- Emby person payloads expose an image tag and the public image handler
  currently resolves the stored `ProfileURL` through `ImageProxy`.

## Requirements

1. Store a person profile image in
   `App.DataDir/people/sha256/<first-two>/<next-two>/<sha256>.<ext>` using
   content hashing, image validation, and atomic writes consistent with the
   existing artwork store.
2. Keep the remote `ProfileURL` as the source reference, and store the local
   profile-image key on `Person` so the image can be served without scanning
   the people directory.
3. Newly persisted credits with a profile URL must download and persist the
   profile image before the person is exposed through Emby. A failed download
   must not erase an existing local image or fail the authoritative metadata
   scrape.
4. Person image downloads must bypass `cache/images` entirely. Successful
   bytes are written directly to `DataDir/people`; no startup migration or
   image-request lazy download is required because the feature is not yet in
   production.
5. `GET /Items/:id/Images/Primary` and its `/emby` and case variants must
   serve the persisted person file directly. Missing or failed images return
   the existing transparent placeholder; the client must never receive a
   third-party image URL.
6. Existing movie/series artwork behavior, Emby payload shape, and source
   profile URLs remain compatible.
7. Cloud sidecar posters and backdrops selected during metadata persistence
   must resolve and download directly into `DataDir/artwork`; persistence must
   not read from or write to `cache/images`.

## Acceptance Criteria

- [ ] A new TMDb person with a valid profile URL has a file under
      `DataDir/people/sha256/...` and a persisted local key after metadata
      persistence completes.
- [ ] Repeated references to the same image use the same content-hash path
      and do not create duplicate bytes.
- [ ] Remote person-image persistence creates no file or failure marker under
      `cache/images`.
- [ ] Emby person image requests serve local bytes, including `HEAD`, both
      route prefixes, and both route casings.
- [ ] Missing image, invalid upstream data, timeout, and download failure
      return the transparent placeholder while preserving any prior local
      image and allowing the metadata scrape to succeed.
- [ ] Cache cleanup does not remove persisted person images.
- [ ] Cloud sidecar posters and backdrops persist under `DataDir/artwork` and
      project as `/api/artwork/{assetID}` without requiring a scanner cache hit.
- [ ] Cloud metadata artwork import creates no file or failure marker under
      `cache/images`.
- [ ] Focused model, repository, storage, service, and Emby handler tests
      cover the new path and all existing tests remain green.

## Out of Scope

- Changing the `ArtworkAsset` schema or removing discovery/proxy image caches.
- Exposing a new public people-image API; existing Emby image routes remain
  the client contract.
- Automatic deletion of unreferenced old profile-image files; cleanup can be
  added after reference accounting exists.
- Migration or backfill for people created before this feature ships.
