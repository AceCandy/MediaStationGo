# Design: Persist Emby People Images

## Boundaries

`TMDb/NFO credit source -> ScraperService -> PersonRepository + PeopleImageStore -> DataDir/people -> Emby image handler`

The existing `ArtworkStore` remains responsible for `MetadataItem` artwork.
People use a separate store because `ArtworkAsset` is tied to
`MetadataArtwork{MetadataID, ArtworkType}` and its root is fixed to
`DataDir/artwork`.

Cloud sidecar URLs remain scan hints until metadata persistence. At that point
`ArtworkStore` resolves the cloud reference, downloads the image without the
proxy cache, and saves it as managed artwork. Scanner stores only the sidecar
reference and does not prefetch poster or backdrop bytes; the generic cloud
image route may still cache explicit client requests for unresolved previews.

## Data Model

- Add nullable `Person.ProfileImageKey`, containing a validated relative key
  such as `sha256/ab/cd/<hash>.jpg`.
- Keep `Person.ProfileURL` unchanged as the source URL and refresh input.
- GORM's existing `AllModels`/startup `AutoMigrate` adds the new nullable
  column for existing databases; no new table or person-artwork join is
  needed because a person has one current profile image.

## PeopleImageStore

- Construct it from `App.DataDir` and reuse the existing outbound image HTTP
  transport and validation without invoking the `ImageProxy` cache API.
- Fetch remote HTTP(S) sources directly, validate the image, calculate the
  SHA-256 content hash, derive the two-level sharded key, and atomically write
  under `DataDir/people`. Do not read or write `cache/images` or its failure
  markers.
- Reuse/extract the existing image validation, extension, hash, and atomic
  write helpers rather than maintaining a second algorithm.
- Serve by a `ProfileImageKey` only after validating that the resolved path is
  within the people root. Never accept a raw filesystem path from a request.

## Persistence Flow

1. Credit persistence prepares profile-image records before the repository
   transaction so network I/O is never held inside a database transaction.
2. Successful downloads pass the local key through `CreditInput`; person
   upsert writes it with `ProfileURL`.
3. A failed new download passes an empty key for a new person, or the current
   key for an existing person, and logs the failure without failing metadata
   persistence.
4. On a person image request, the handler serves only the stored key. A missing
   or invalid key returns the existing placeholder; requests never trigger
   network I/O.

## Emby Contract

The JSON contract remains `ImageTags.Primary = person.ID`. The client continues
to request `/Items/{id}/Images/Primary`; the handler detects a person and
serves `PeopleImageStore` bytes. No third-party URL is redirected or returned.

Movie and series metadata project selected posters and backdrops as
`/api/artwork/{assetID}`. Emby serves those URLs from `DataDir/artwork`; the
original cloud URL is retained only as artwork provenance.

## Failure and Compatibility

- A failed refresh never clears an existing key.
- A changed source URL replaces the key only after the new bytes are
  validated and written successfully.
- Empty `ProfileURL` means no profile image; the existing transparent
  placeholder is returned.
- Existing `cache/images` remains a remote proxy cache for other callers, but
  person import neither reads nor writes it.
- Old profile files are retained when references change; deletion is deferred
  to avoid deleting a deduplicated file still referenced by another person.

## Rollback

The feature can be rolled back by removing the people-store wiring while
leaving `ProfileImageKey` and files unused. No migration rollback is needed.
