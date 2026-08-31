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

- Apply this contract when changing Douban poster parsing, `api_configs.base_url` for Douban, managed Douban artwork candidates, or the Douban artwork repair task.

### 2. Signatures

- Configuration: `PUT /api/admin/api-configs/douban` with optional `{"base_url":"http://db-pic1.acecandy.cn/"}`.
- Runtime resolution: `APIConfigService.Resolve(ctx, "douban").BaseURL`.
- Manual task definition: `douban_artwork_local_repair`, displayed as `豆瓣图片本地化修复`.
- Candidate repair CAS identity: candidate ID plus metadata ID, artwork type, old asset ID, provider, and old source URL; current-selection synchronization is guarded by metadata ID, artwork type, old asset ID, and Douban provider.

### 3. Contracts

- Douban `base_url` is an optional image origin. It never changes the Douban JSON API host or Cookie behavior.
- Resolve it for each image download. When configured, replace only the returned image URL's scheme and host; preserve path and query. An empty value keeps the original image URL.
- Select new posters in this order: `cover.image.large.url`, `pic.large`, `pic.normal`, then compatible legacy fields. A non-HTTP(S) value is invalid and must not block the next fallback.
- Managed image bytes still pass through `ImageProxy.Fetch` and `ArtworkStore`, including content validation, the 32 MiB limit, dimensions, SHA-256 deduplication, and DataDir storage.
- The repair task scans Douban poster candidates. A missing local file, `s_ratio_poster` source, or asset width at most 300 is repairable. Known `/view/photo/<variant>/public/<file>` paths may be changed to `/view/photo/l/public/<file>` for historical repair.
- Download and prepare the replacement before a transactional candidate CAS. Update the current selection only when it still points to the same old Douban asset. Never delete the old asset row or file in this task.
- The scheduler job exists for task-center execution but is disabled by default. Saving `base_url` never starts repair work.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Empty Douban `base_url` | Keep the upstream image origin. |
| Absolute HTTP(S) origin with no credentials, query, fragment, or non-root path | Normalize and save its scheme and host. |
| Invalid/non-HTTP(S) origin | Return HTTP 400 from the admin update and keep the previous effective value. |
| Preferred poster field is invalid | Continue to the next poster field. |
| Replacement download/content validation fails | Keep candidate, current selection, old asset row, and old file unchanged. |
| Candidate changes during download | CAS updates zero rows; record a concurrent skip. |
| Current selection belongs to another provider or asset | Update only the Douban candidate; preserve the current selection. |
| Current selection still matches the old Douban candidate | Switch candidate and current selection in the same transaction. |

### 5. Good / Base / Bad Cases

- Good: a configured mirror downloads `/view/photo/l/public/p123.jpg` from the mirror while the Douban detail request still uses `m.douban.com`.
- Base: no image origin is configured; large posters download from the URL returned by Douban.
- Good: 713 non-selected Douban candidates upgrade without changing their other-provider current posters, while a matching selected Douban poster follows its upgraded candidate.
- Bad: rewrite the Douban JSON API base URL, derive every new poster by blind string replacement, or delete the small file before the large image is safely stored.

### 6. Tests Required

- Unit-test poster field priority, invalid-field fallback, origin validation/normalization, scheme-host replacement, and historical `/l/public/` derivation.
- Repository-test keyset candidate projection and CAS behavior for stale candidates, other-provider current selections, and matching Douban current selections.
- Service-test a small or missing candidate through download, DataDir storage, configured origin use, candidate switch, and old-file preservation.
- Assert task definition and disabled scheduler registration; run `go vet`, focused service/repository tests, and `git diff --check`.

### 7. Wrong vs Correct

```go
// Wrong: the first non-empty object value selects pic.normal and keeps a small poster.
poster := firstStringFromMap(subject, "pic", "cover")

// Correct: validate each explicitly sized field and fall back in size order.
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
