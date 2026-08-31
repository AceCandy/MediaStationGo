# Douban Configuration and Artwork Contract

## 1. Scope / Trigger

Read this contract when changing Douban authentication, the external API configuration UI/API, `DoubanProvider`, or legacy secrets/environment wiring.

## 2. Signatures

```http
PUT /api/admin/api-configs/douban
Content-Type: application/json

{"api_key":"<cookie>","enabled":true}
```

```http
DELETE /api/admin/api-configs/douban
```

```go
APIConfigService.Resolve(ctx, "douban") (Resolved, error)
```

The value is encrypted in the `text` column `api_configs.api_key`. Public responses expose only `has_key` and `masked_key`, never `api_key`.

## 3. Contracts

- `api_configs` is the only Douban Cookie source; no `secrets.douban_cookie` field or environment key exists.
- Both historical Go models mapped to `api_configs` must declare `api_key` as `text`; a startup compatibility migration widens legacy `varchar(512)` columns without changing stored values.
- The Web UI labels the Douban credential as `Cookie`, keeps a password input, and submits it through `api_key`.
- Every Douban search, detail attempt, and discover request resolves the current row immediately before sending.
- Set the outbound `Cookie` header only when the resolved row is enabled and the decrypted value is non-empty.
- Deleting the credential clears the encrypted column without deleting the provider row.

## 4. Validation & Error Matrix

| Condition | Outbound behavior |
| --- | --- |
| Enabled row with a non-empty credential | Send the complete `Cookie` header. |
| Missing row, empty credential, or disabled row | Send anonymously. |
| Database lookup/decryption path returns an error | Log no credential data and send anonymously. |
| Credential is updated or cleared | The next request observes the new state without restart. |
| Legacy `api_key varchar(512)` column | Widen idempotently to `text` during startup migration. |

## 5. Good / Base / Bad Cases

- Good: an administrator replaces the Cookie and the next request uses it while public API responses remain masked.
- Base: no Cookie is configured and public Douban endpoints continue anonymously.
- Bad: reading a startup secret, caching the decrypted value, logging an outbound header, or letting duplicate models declare different column types.

## 6. Tests Required

- Exercise search, both detail attempts, and discover through a transport that checks the expected header state.
- On one provider instance, verify missing, configured, updated, cleared, disabled, and lookup-error states.
- Start from a PostgreSQL `varchar(512)` column, run migration twice, assert `text`, and save a value longer than 512 characters.
- Run Web lint/build and confirm legacy field/environment names have no product-code matches.

## 7. Wrong vs Correct

### Wrong

```go
req.Header.Set("Cookie", cfg.Secrets.DoubanCookie)
```

### Correct

```go
resolved, err := apiConfig.Resolve(ctx, "douban")
if err == nil && resolved.Enabled && strings.TrimSpace(resolved.APIKey) != "" {
	req.Header.Set("Cookie", strings.TrimSpace(resolved.APIKey))
}
```

## Scenario: Configurable Douban Artwork Origin and Large Poster Repair

### 1. Scope / Trigger

- Apply this contract when changing Douban poster parsing, `api_configs.base_url` / `api_configs.image_direct` for Douban, image transport selection, managed Douban artwork candidates, or the Douban artwork repair task.

### 2. Signatures

- Configuration: `PUT /api/admin/api-configs/douban` with optional `{"base_url":"http://db-pic1.acecandy.cn/","image_direct":true}`.
- Public response: `PublicView.ImageDirect` serializes as `image_direct`; old rows default to `false`.
- Runtime resolution: `APIConfigService.Resolve(ctx, "douban")` carries `BaseURL` and `ImageDirect`.
- Artwork projection: `DoubanProvider.ResolveArtworkURL(ctx, sourceURL)`.
- Manual task definition: `douban_artwork_local_repair`, displayed as `豆瓣图片本地化修复`.
- Candidate repair CAS identity: candidate ID plus metadata ID, artwork type, old asset ID, provider, and old source URL; current-selection synchronization is guarded by metadata ID, artwork type, old asset ID, and Douban provider.

### 3. Contracts

- Douban `base_url` is an optional image origin. It never changes the Douban JSON API host or Cookie behavior.
- Known `/view/photo/<variant>/public/<file>` paths resolve to `/view/photo/l/public/<file>`. Search `img` and discover `cover` are thumbnail fields and must be omitted when this known large path cannot be derived.
- Select detail posters only in this order: `cover.image.large.url`, then `pic.large`. A non-HTTP(S) value is invalid and must not block the next large field; `pic.normal` and compatible thumbnail fields are not fallbacks.
- Resolve the current image origin for search display, discover display/preheat, enrichment downloads, and repair downloads. When configured and enabled, replace the image URL's scheme and host, change the final path extension to `.webp`, and change observed Qiniu `/format/jpg` or `/format/jpeg` query segments to `/format/webp`. An extensionless valid large URL changes origin only.
- An empty/disabled/unavailable image-origin configuration keeps the upstream origin and format, but known photo paths still use the `/l/` variant.
- Discover section cache stores the origin-independent large URL. Apply `ResolveArtworkURL` to cloned results before response, image preheat, and catalog hydration so a configuration update is visible on the next cached response without cache invalidation.
- Artwork URL projection is idempotent. Reprocessing an `/l/`, configured-origin, `.webp` URL does not change it again.
- Managed image bytes still pass through `ImageProxy.Fetch` and `ArtworkStore`, including content validation, the 32 MiB limit, dimensions, SHA-256 deduplication, and DataDir storage.
- `image_direct` changes only Douban image transport. On a cache miss or explicit refresh, an enabled option applies to official `doubanio.com` hosts and the host of the current Douban `base_url`; it does not change Douban metadata requests or unrelated image hosts.
- Direct image mode skips the proxy-aware default client, uses `NewInternalTransport`, then permits the existing curl fallback for the provider-scoped URL regardless of whether its host is official or configured. Normal mode keeps `default -> direct -> official-host curl`.
- A configuration lookup error falls back to normal mode. A valid cached image is served without resolving transport configuration.
- Normal and direct modes share successful image bytes but use separate six-hour failure markers. Enabling direct mode therefore bypasses an old normal-mode failure immediately; direct failures remain negatively cached. `RemoveFailed` and `RemoveCached` clear both markers.
- The repair task scans Douban poster candidates. A missing local file, `s_ratio_poster` source, or asset width at most 300 is repairable. Known `/view/photo/<variant>/public/<file>` paths may be changed to `/view/photo/l/public/<file>` for historical repair.
- Download and prepare the replacement before a transactional candidate CAS. Update the current selection only when it still points to the same old Douban asset. Never delete the old asset row or file in this task.
- The scheduler job exists for task-center execution but is disabled by default. Saving `base_url` never starts repair work.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Empty, disabled, or unreadable Douban `base_url` | Keep the upstream image origin and file format; still derive known `/l/` paths. |
| Absolute HTTP(S) origin with no credentials, query, fragment, or non-root path | Normalize and save its scheme and host. |
| Invalid/non-HTTP(S) origin | Return HTTP 400 from the admin update and keep the previous effective value. |
| `cover.image.large.url` is invalid | Continue to `pic.large`. |
| Only detail thumbnail/legacy fields exist | Return no poster; never download the thumbnail. |
| Search/discover thumbnail has a known photo path | Derive `/view/photo/l/public/<file>`; apply current origin/WebP only when returning it. |
| Search/discover thumbnail has no known large path | Return an empty poster URL. |
| Configured-origin URL ends in an image extension and query format is JPEG | Change the path extension and observed query format to WebP. |
| Valid configured-origin detail large URL has no extension | Change origin only; do not guess or append an extension. |
| Discover cache predates an image-origin update | Keep the cached origin-independent URL and project the new origin on the next response/preheat. |
| `image_direct=false`, missing row, or configuration lookup error | Keep the existing proxy-aware image request order. |
| `image_direct=true` with an official or current configured image host | Try only the proxy-free Go client, then curl if it fails. |
| `image_direct=true` with an unrelated image host | Keep the existing request order; never broaden direct mode to that host. |
| Normal-mode failure marker exists when direct mode is enabled | Ignore that marker, attempt the direct route, and retain a separate direct-mode failure marker on failure. |
| Replacement download/content validation fails | Keep candidate, current selection, old asset row, and old file unchanged. |
| Candidate changes during download | CAS updates zero rows; record a concurrent skip. |
| Current selection belongs to another provider or asset | Update only the Douban candidate; preserve the current selection. |
| Current selection still matches the old Douban candidate | Switch candidate and current selection in the same transaction. |

### 5. Good / Base / Bad Cases

- Good: a configured mirror displays and downloads `/view/photo/l/public/p123.webp` while the Douban detail request still uses `m.douban.com`.
- Good: changing the mirror immediately changes a cached discover response and its preheat URL without another Douban request.
- Good: a configured mirror that fails through the proxy is fetched with a proxy-free Go client and then curl, while TMDb images keep their existing transport order.
- Base: no image origin is configured; search/discover use the upstream `/l/` JPEG and detail uses a validated large field.
- Base: `image_direct` is absent or false and existing deployments keep the previous image behavior.
- Good: 713 non-selected Douban candidates upgrade without changing their other-provider current posters, while a matching selected Douban poster follows its upgraded candidate.
- Bad: infer provider identity only from `doubanio.com` after the URL was rewritten to a configured mirror, route every image through curl, rewrite the Douban JSON API base URL, return `pic.normal`, cache the configured mirror URL for six hours, derive a non-standard thumbnail by guesswork, or delete the small file before the large image is safely stored.

### 6. Tests Required

- Unit-test large-only poster field priority, invalid-field fallback, nested thumbnail rejection, origin validation, `/l/public/` derivation boundaries, WebP path/query rewriting, extensionless behavior, and idempotency.
- Provider/handler-test search and discover thumbnail derivation plus live image-origin projection on cached results; assert the cache retains its origin-independent URL.
- Repository-test keyset candidate projection and CAS behavior for stale candidates, other-provider current selections, and matching Douban current selections.
- Service-test a small or missing candidate through download, DataDir storage, configured origin use, candidate switch, and old-file preservation.
- Assert the `image_direct` database migration and API update/public/resolve round trip.
- Image-proxy-test that enabled official/configured hosts select only a proxy-free direct client and allow curl, disabled/custom-unrelated hosts keep normal behavior, and cache cleanup covers both failure markers.
- Assert task definition and disabled scheduler registration; run `go vet`, focused service/repository tests, and `git diff --check`.

### 7. Wrong vs Correct

```go
// Wrong: the first non-empty object value selects pic.normal and keeps a small poster.
poster := firstStringFromMap(subject, "pic", "cover")

// Correct: validate only explicitly large fields and never fall back to a thumbnail.
poster := doubanPosterURL(subject)
```

```go
// Wrong: change the candidate before the replacement image is validated.
updateCandidate(newURL)
download(newURL)

// Correct: prepare bytes first, then use the old candidate snapshot as the CAS guard.
asset := prepareDownloadedArtwork(newURL)
repairDoubanCandidate(snapshot, newURL, asset)
```

```go
// Wrong: a configured mirror no longer contains doubanio.com, so it never reaches curl.
if isDoubanImageHost(host) {
	fetchRemoteImageWithCurl(ctx, raw, host)
}

// Correct: first prove the URL belongs to the configured Douban image source,
// then let direct mode use curl without a second fixed-host restriction.
directOnly := imageProxy.useDoubanImageDirect(ctx, host)
if imageProxy.canUseExternalImageFallback(directOnly, host) {
	fetchRemoteImageWithCurl(ctx, raw, host)
}
```
