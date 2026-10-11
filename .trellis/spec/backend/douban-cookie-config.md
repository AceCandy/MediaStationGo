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

## Scenario: Legacy Douban Candidate Selection

The following signatures describe the legacy generic search interface. The metadata editor no longer binds Douban IDs: dedicated binding uses complete webpage search and immediate enrichment, as specified in [Manual Douban Binding](./shared-media-metadata.md#scenario-manual-douban-binding). Do not route dedicated manual binding back through suggest.

### 1. Scope / Trigger

- Apply this contract when changing Douban suggest parsing, manual provider search, or metadata-editor Douban ID selection.

### 2. Signatures

- Provider candidates: `DoubanProvider.SearchCandidates(ctx, query) ([]*DoubanMatch, error)`.
- Compatibility lookup: `DoubanProvider.Search(ctx, query) (*DoubanMatch, error)` returns only the first candidate.
- API: `GET /media/:id/scrape/search?provider=douban&query=<title>` returns `{"items":[ManualScrapeCandidate...]}`.

### 3. Contracts

- Parse the complete `subject_suggest` array for manual search; each candidate carries `title`, optional `year` and poster, and `douban_id`.
- Keep automatic scraping and other existing `Search` / `SearchMatch` consumers on the first-result behavior.
- Selecting a candidate only updates the metadata editor's local `douban_id`; persistence happens through the existing metadata Save action.
- Ignore candidates without `douban_id`. Do not change TMDb, Bangumi, or TheTVDB search behavior.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Empty title | Keep the Douban search action disabled. |
| Empty suggest array | Show an empty-result state and leave the form unchanged. |
| Search request or parsing fails | Show an error state and leave the form unchanged. |
| Candidate has no `douban_id` | Do not offer or perform ID selection. |
| A closed or superseded request completes | Ignore its result. |

### 5. Good / Base / Bad Cases

- Good: multiple same-title entries are shown and the administrator selects the correct year and ID before saving.
- Base: no candidates are found and the existing form values remain intact.
- Bad: auto-select the first result, persist on candidate click, or change automatic scraping to consume every suggestion.

### 6. Tests Required

- Assert that manual Douban lookup returns multiple candidates and handles an empty array.
- Assert that legacy `Search` still returns the first candidate and preserves status-error behavior.
- Run Web lint/build and verify selection changes only `douban_id`; review stale-response invalidation for the stacked modal.

### 7. Wrong vs Correct

```go
// Wrong: changing Search to return a slice breaks automatic scraping callers.
matches, err := provider.Search(ctx, query)

// Correct: manual search opts into candidates while Search remains first-result compatible.
matches, err := provider.SearchCandidates(ctx, query)
```

## Scenario: Explicit Douban Proxy Pool

### 1. Scope / Trigger

- Apply this contract when changing external API proxy storage, the Douban `use_proxy_pool` option, or Douban Search/Discover/Detail JSON transports.
- The pool is not a global HTTP fallback: images, AI, and every other Provider keep their existing clients.

### 2. Signatures

- Configuration: `PUT /api/admin/api-configs/douban` with optional `{"use_proxy_pool":true}`.
- Pool API: `GET|PUT /api/admin/api-proxy-pool`; PUT accepts `{"items":[{"id":"existing-id"},{"url":"http://user:pass@host:8080"}]}`.
- Pool response item: `{"id":"...","display_url":"http://host:8080","has_auth":true}`.
- Database: `api_configs.use_proxy_pool boolean DEFAULT false`; `proxy_pool_entries(url text, position integer)` where `url` is an AES-GCM ciphertext.

### 3. Contracts

- The Web proxy editor is one multiline textarea below the Provider table; every non-empty line is one ordered proxy. An unchanged `display_url` submits only its existing `id`, while a new line submits `url`. Prefix a line with `!` to force URL replacement, including replacing an authenticated entry with its credential-free display URL. Successful saves repopulate the textarea only with credential-free `display_url` values.
- PUT replaces the complete ordered list transactionally. Missing existing IDs are physically deleted; retained IDs keep their encrypted URL. After validating and normalizing every item, it keeps the first occurrence of each complete URL and silently drops later duplicates. The comparison includes scheme and Userinfo, so distinct credentials for the same endpoint remain separate proxies.
- Enabling the option starts on `NewInternalTransport()` (`Transport.Proxy=nil`), never on environment or system proxies. Disabling it keeps the original proxy-aware Douban client.
- Every movie-enrichment task starts at direct. Exact HTTP 400 or `unexpected EOF` from direct selects only the first configured proxy; a successful proxy remains sticky within that task.
- Any HTTP status of 400 or higher, request error, or body-read error from a proxy immediately retries direct once. Return that final direct result without trying another proxy or returning to the proxy pool. Proxy generation and Douban configuration revision changes also reset the next enabled request to direct.
- Cookie resolution and headers remain per attempt; no Cookie, proxy Userinfo, complete credential URL, or ciphertext may enter logs, errors, task output, or API responses.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Empty pool, direct returns 400 | Execute the final direct attempt, so direct runs twice. |
| `http`, `https`, `socks5`, or `socks5h` URL with a hostname | Normalize, encrypt, and save it. |
| Missing hostname, unsupported scheme, query, fragment, or non-root path | Reject the complete PUT without changing storage or the runtime snapshot. |
| Unknown/duplicate retained ID | Return a positional validation error without echoing the URL or credentials. |
| Multiple items normalize to the same complete URL | Keep the first item, drop later duplicates, and compact stored positions. |
| Textarea line equals an existing credential-free `display_url` | Submit its unused existing ID and preserve the encrypted URL; prefix `!` to force replacement instead. |
| Proxy returns no HTTP response | Retry direct once for every request error. |
| Proxy response body cannot be read | Retry direct once without making that proxy sticky. |
| Proxy returns HTTP 400 or higher | Retry direct once; do not try the next saved proxy. |
| Pool read/decrypt fails while enabled | Return a configuration error; never fall back to the environment proxy. |

### 5. Good / Base / Bad Cases

- Good: repeated saves containing the same normalized complete URL persist one ordered entry, while the same endpoint with different credentials remains separate.
- Good: direct 400, proxy 1 returns 200, and later requests reuse proxy 1 until any proxy failure returns routing to direct.
- Base: the option is false, so the pre-existing environment/system-proxy-aware client remains unchanged.
- Bad: persist a normalized complete URL more than once, deduplicate by credential-free `display_url`, try another proxy after the selected proxy fails, carry a proxy route into the next enrichment task, expose a masked credential as a replacement value, put proxy URLs in `APIConfig.Extra`, or share Douban's sticky route with another Provider.

### 6. Tests Required

- Assert encrypted persistence, physical deletion, stable ordering, normalized complete-URL deduplication, preservation of credential-distinct URLs, transactional invalid-input failure, safe JSON/error projection, and all four accepted schemes.
- Assert the multiline editor ignores empty lines, preserves unused IDs for unchanged display URLs, keeps duplicate display URLs distinct, and treats `!` lines as forced URL replacements.
- Assert the option defaults false and update/public/resolve round-trips; same-value Douban saves must advance the runtime revision.
- Exercise direct success, one-proxy selection, sticky reuse, proxy 4xx/5xx and network/read failure returning direct, skipped remaining proxies, final direct failure, empty-pool double direct, `unexpected EOF` selection, per-task direct reset, generation/revision resets, and concurrent failure serialization.
- Exercise Search, Discover, ordinary Detail, and enrichment Detail without changing their existing status errors, parsing, Cookie, or permission fallback behavior.

### 7. Wrong vs Correct

```go
// Wrong: a global transport silently rotates every external Provider on any failure.
if err != nil || resp.StatusCode >= 400 {
	useNextProxy()
}

// Correct: direct has narrow selection triggers; a selected proxy falls back on any request failure.
if !shouldReselectDoubanRoute(route, result) {
	return result
}
return d.reselectAfterRouteFailure(...)
```

## Scenario: Resin Proxy Gateway

### 1. Scope / Trigger

- Apply this contract when changing the global proxy-pool selector, Resin gateway configuration, or Douban Resin URL rewriting.
- Resin is one URL reverse-proxy gateway. Its internal nodes, health checks, scheduling, and circuit breaking remain owned by Resin.

### 2. Signatures

- Douban configuration: `PUT /api/admin/api-configs/douban` uses only `use_proxy_pool`; provider UI does not own the proxy type or Resin fields.
- Global configuration: `GET|PUT /api/admin/api-proxy-pool/config` uses `proxy_pool_type` (`normal` or `resin`), `resin_proxy_url`, optional `resin_proxy_token`, and optional `resin_account`.
- The public proxy-pool response returns the type, credential-free Resin origin, Account, and `has_resin_proxy_token`; it never returns the token or ciphertext.
- For rollback compatibility, the global service physically reuses `api_configs.proxy_pool_type`, `resin_proxy_url`, encrypted `resin_proxy_token`, and `resin_account`. The UI and runtime ownership still belong to `ProxyPoolService`.

### 3. Contracts

- Historical rows and an empty `proxy_pool_type` resolve to `normal`; existing manual pool behavior stays unchanged.
- `resin_proxy_url` is an HTTP(S) origin without Userinfo, path, query, or fragment. Store `resin_proxy_token` with the existing AES-GCM service.
- Resin mode rewrites each original target to `<origin>/<escaped-token>/<identity>/<scheme>/<host>/<escaped-path>?<query>` and sends it as a normal HTTP request. Never set `http.Transport.Proxy` for this route.
- Preserve the original target's escaped path and raw query. An empty Account uses identity `.` for Default-platform random routing; a non-empty Account uses the escaped `Default.<account>` identity.
- Use the shared enrichment-task routing sequence: direct first, Resin only after direct HTTP 400 / `unexpected EOF`, then one final direct attempt after any Resin request failure. Never fall back from Resin mode to the manual pool.
- A global proxy configuration revision or Douban configuration revision resets routing to direct. Switching modes never deletes manual entries.
- Resin Token, complete reverse URL, Cookie, and upstream request URL never enter MediaStationGo API responses, logs, or errors. Sanitize `url.Error.URL` while preserving the underlying error for `errors.Is` classification.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Proxy type omitted or empty | Use `normal`. |
| Proxy type is neither `normal` nor `resin` | Reject without changing the row. |
| Resin selected with no address or stored Token | Reject without changing the row. |
| Invalid or credential-bearing Resin address | Reject without echoing the input. |
| Token omitted or empty on update | Preserve the encrypted stored token. |
| Account empty | Build the `./` identity path for Default-platform random routing. |
| Account configured | Build the escaped `Default.<account>/` identity path. |
| Resin URL construction or request fails | Retry direct once; keep the Resin error credential-free and do not use the manual pool. |

### 5. Tests Required

- Assert old rows remain readable, empty types default to `normal`, and existing Resin data needs no re-entry.
- Assert Resin Token encryption, safe public projection, omitted-token preservation, address/type validation, and live global revision.
- Exercise empty and configured Account URL rewriting, including escaped target paths and raw queries.
- Assert Resin selection rewrites only the Resin request after direct failure, configuration changes reset to direct, and all manual proxy routing tests remain green.
- Run Web lint/build, focused Go tests, `go vet ./internal/service ./internal/handler`, and `git diff --check`.

## Scenario: Manual Proxy Pool Health Cleanup

### 1. Scope / Trigger

- Apply this contract when changing saved-proxy health checks, cleanup classification, or the administrator proxy-pool panel.
- This is an explicit administrator action, not a scheduler, persistent health score, or provider-wide proxy fallback.

### 2. Signatures

- Check: `POST /api/admin/api-proxy-pool/check` with no request body.
- Check response: `{"total":0,"available":0,"unavailable":0,"inconclusive":0,"cleanup_token":"..."}`; omit `cleanup_token` when no proxy is removable.
- Cleanup: `POST /api/admin/api-proxy-pool/cleanup` with `{"token":"..."}`.
- Cleanup response: `{"items":[<credential-free pool items>],"removed":1}`.

### 3. Contracts

- Check only persisted encrypted entries; unsaved textarea edits never participate.
- First probe the same lightweight Douban JSON endpoint through a direct external client. If that baseline is not healthy, fail the complete check and issue no cleanup token.
- Run saved proxies with bounded workers and a per-proxy timeout without holding `ProxyPoolService.mu` during network I/O.
- A cleanup token is server-owned, single-use after successful cleanup, valid for five minutes, and bound to the unavailable IDs and current proxy-pool generation. Any pool save invalidates it.
- Cleanup reuses transactional full replacement, preserves retained order and encrypted credentials, and returns the refreshed credential-free pool.
- Cleanup bypasses PUT's URL deduplication so a token removes only its bound IDs and never collapses unrelated historical duplicates.
- API errors and responses never include a complete proxy URL, Userinfo, ciphertext, Cookie, or upstream request URL.
- While a check is running, disable pool editing and saving. If edits were already unsaved when the check began, warn that confirmed cleanup replaces the textarea with the saved result.

### 4. Validation & Error Matrix

| Condition | Classification / result |
| --- | --- |
| HTTP 200 with valid JSON | `available` |
| Connection/transport failure, timeout, HTTP 407, exact HTTP 400, `unexpected EOF`, or HTTP 200 with invalid JSON | `unavailable` and eligible for the token |
| HTTP 403, 429, 5xx, oversized body, or a body-read error other than `unexpected EOF` | `inconclusive`; never removable |
| Direct baseline is not `available` | Fail the complete check and issue no token |
| Request context is canceled | Stop taking work, return an error, and do not mutate the pool |
| Missing, expired, unrelated, reused, or stale-generation token | Reject cleanup and require another check |

### 5. Good / Base / Bad Cases

- Good: the direct baseline succeeds, two dead proxies produce a token, the administrator confirms, and only those two persisted IDs are deleted.
- Base: every saved proxy is available or inconclusive, so the UI shows the summary and performs no cleanup request.
- Bad: accept proxy IDs from the browser, classify rate limiting as dead, keep a token valid after a save, or overwrite edits made while the health request is in flight.

### 6. Tests Required

- Service tests classify success, connection errors, 400, 407, `unexpected EOF`, invalid JSON, other read errors, 403, 429, 5xx, cancellation, and baseline failure.
- Assert check does not change persistence, a concurrent save invalidates its result, and cleanup removes only token-bound IDs while preserving retained order and unrelated historical duplicates.
- Handler tests assert both administrator routes exist and cleanup errors cannot expose stored credentials.
- Run `go test ./internal/service`, focused proxy-pool handler tests, `go vet ./internal/service ./internal/handler`, Web lint/build, and `git diff --check`.

### 7. Wrong vs Correct

```go
// Wrong: the browser chooses arbitrary rows to delete.
cleanup(request.UnavailableIDs)

// Correct: the server resolves IDs from its short-lived generation-bound result.
cleanup(request.Token)
```

## Scenario: Configurable Douban Artwork Origin and Large Poster Repair

### 1. Scope / Trigger

- Apply this contract when changing Douban poster parsing, `api_configs.base_url` / `api_configs.image_direct` for Douban, image transport selection, managed Douban artwork candidates, or the Douban artwork repair task.

### 2. Signatures

- Configuration: `PUT /api/admin/api-configs/douban` with optional `{"base_url":"http://db-pic1.acecandy.cn/","image_direct":true}`.
- Public response: `PublicView.ImageDirect` serializes as `image_direct`; old rows default to `false`.
- Runtime resolution: `APIConfigService.Resolve(ctx, "douban")` carries `BaseURL` and `ImageDirect`.
- Artwork projection: `DoubanProvider.ResolveArtworkURL(ctx, sourceURL)`.
- Download boundary: `ImageProxy.Fetch(ctx, officialSourceURL)`.
- Manual task definition: `douban_artwork_local_repair`, displayed as `豆瓣图片本地化修复`.
- Candidate repair CAS identity: candidate ID plus metadata ID, artwork type, old asset ID, provider, and old source URL; current-selection synchronization is guarded by metadata ID, artwork type, old asset ID, and Douban provider.
- Candidate checkpoint: `metadata_artwork_candidates.repair_checked_url text NOT NULL DEFAULT ''`; `MarkDoubanCandidateRepairChecked` uses the same candidate CAS identity.

### 3. Contracts

- Douban `base_url` is an optional download CDN. It never changes the Douban JSON API host, Cookie behavior, API responses, discovery cache, or persisted artwork source URL.
- Known `/view/photo/<variant>/public/<file>` paths resolve to `/view/photo/l/public/<file>`. Search `img` and discover `cover` are thumbnail fields and must be omitted when this known large path cannot be derived.
- Select detail posters only in this order: `cover.image.large.url`, then `pic.large`. A non-HTTP(S) value is invalid and must not block the next large field; `pic.normal` and compatible thumbnail fields are not fallbacks.
- `ResolveArtworkURL` returns the stable official large URL. It removes a complete known `imageView2/...` processing query, preserves unknown queries, and never reads `base_url` or changes the source extension.
- Search, discover, provider matches, managed candidate `source_url`, and the image cache key keep that official URL. A configured CDN URL is never persisted or returned.
- On an official Douban cache miss, `ImageProxy` resolves the current enabled `base_url`, temporarily replaces scheme/host, and changes a final extension to `.webp`. An extensionless URL changes origin only; unknown queries remain unchanged.
- Try the temporary CDN URL first and the official URL second. Either success writes bytes under the official URL's cache key, so changing `base_url` affects the next uncached download without database migration.
- A failure marker is written only when the final official request returns exact HTTP 404. CDN 404, DNS/TLS/timeout, 429, 5xx, empty bodies, and non-image responses remain retryable and never create a six-hour marker.
- Managed image bytes still pass through `ImageProxy.Fetch` and `ArtworkStore`, including content validation, the 32 MiB limit, dimensions, SHA-256 deduplication, and DataDir storage.
- `image_direct` changes only Douban image transport. On a cache miss or explicit refresh, an enabled option applies to official `doubanio.com` hosts and the host of the current Douban `base_url`; it does not change Douban metadata requests or unrelated image hosts.
- Direct image mode skips the proxy-aware default client, uses `NewInternalTransport`, then permits the existing curl fallback for the provider-scoped URL regardless of whether its host is official or configured. Normal mode keeps `default -> direct -> official-host curl`.
- A configuration lookup error falls back to normal mode. A valid cached image is served without resolving transport configuration.
- Normal and direct modes share successful image bytes but use separate six-hour 404 markers. Enabling direct mode therefore bypasses an old normal-mode 404 immediately. `RemoveFailed` and `RemoveCached` clear both markers.
- The repair task scans Douban poster candidates. A missing local file, `s_ratio_poster` source, or asset width at most 300 is repairable. Known `/view/photo/<variant>/public/<file>` paths may be changed to `/view/photo/l/public/<file>` for historical repair.
- `repair_checked_url` is business state, not task history. When the local candidate file is usable and the current resolved official repair URL equals this checkpoint, skip the candidate even when the stored image width is at most 300.
- A successful replacement stores the official URL in both `source_url` and `repair_checked_url` within the candidate CAS. If the final official request returns exact 404 while the existing local image remains usable, keep that image and `source_url`, checkpoint only the attempted official URL, and finish that candidate without failure.
- Exact 404 with no usable local image and every transient failure leave `repair_checked_url` unchanged and remain retryable. A different resolved official URL does not match the old checkpoint and is attempted normally.
- Download and prepare the replacement before a transactional candidate CAS. Update the current selection only when it still points to the same old Douban asset. Never delete the old asset row or file in this task.
- The scheduler job exists for task-center execution but is disabled by default. Saving `base_url` never starts repair work.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Empty, disabled, or unreadable Douban `base_url` | Return/store the official `/l/` URL and download it directly. |
| Absolute HTTP(S) origin with no credentials, query, fragment, or non-root path | Normalize and save its scheme and host. |
| Invalid/non-HTTP(S) origin | Return HTTP 400 from the admin update and keep the previous effective value. |
| `cover.image.large.url` is invalid | Continue to `pic.large`. |
| Only detail thumbnail/legacy fields exist | Return no poster; never download the thumbnail. |
| Search/discover thumbnail has a known photo path | Derive and return `/view/photo/l/public/<file>` on the official origin. |
| Search/discover thumbnail has no known large path | Return an empty poster URL. |
| Official URL has a complete `imageView2/...` query | Remove the complete query before response, persistence, and cache lookup. |
| Official URL has an unknown query | Preserve the query unchanged. |
| Configured CDN is enabled and the official URL ends in an extension | Request the current CDN with a temporary `.webp` path, then fall back to the official URL on any failure. |
| Valid official large URL has no extension | Change origin only for the temporary CDN request; do not guess or append an extension. |
| CDN returns 404 and official source succeeds | Return/cache the official result and write no failure marker. |
| CDN and official source both fail, but official failure is not 404 | Return the final error and write no failure marker. |
| Final official source returns exact 404 | Write the mode-specific six-hour failure marker under the official URL key. |
| Final official source returns exact 404 and the candidate's local image is usable | Keep the existing asset and provenance, CAS the official URL into `repair_checked_url`, and report a non-failing retained-small-image detail. |
| Final official source returns exact 404 and the candidate's local image is missing | Report failure and do not checkpoint the URL. |
| Checked official URL still matches and the local image is usable | Skip without another download or repeated success detail, regardless of stored width. |
| Provider snapshot resolves to a different official URL | Ignore the old checkpoint and attempt the new URL. |
| Image download fails with a non-404 error | Report a retryable failure and do not checkpoint the URL. |
| Discover cache predates an image-origin update | Keep returning the same official URL; the next image-cache miss uses the new CDN. |
| `image_direct=false`, missing row, or configuration lookup error | Keep the existing proxy-aware image request order. |
| `image_direct=true` with an official or current configured image host | Try only the proxy-free Go client, then curl if it fails. |
| `image_direct=true` with an unrelated image host | Keep the existing request order; never broaden direct mode to that host. |
| Normal-mode failure marker exists when direct mode is enabled | Ignore that marker, attempt the direct route, and retain a separate direct-mode failure marker on failure. |
| Replacement download/content validation fails | Keep candidate, current selection, old asset row, and old file unchanged. |
| Candidate changes during download | CAS updates zero rows; record a concurrent skip. |
| Current selection belongs to another provider or asset | Update only the Douban candidate; preserve the current selection. |
| Current selection still matches the old Douban candidate | Switch candidate and current selection in the same transaction. |

### 5. Good / Base / Bad Cases

- Good: a configured mirror downloads temporary `/view/photo/l/public/p123.webp`, while responses and candidate provenance keep the official `/view/photo/l/public/p123.jpg`.
- Good: changing the mirror leaves cached discover data and database rows untouched; the next uncached download uses the new mirror.
- Good: a mirror DNS failure falls back to the official URL in the same request and is retried on the next run if both paths fail transiently.
- Good: a configured mirror that fails through the proxy is fetched with a proxy-free Go client and then curl, while TMDb images keep their existing transport order.
- Base: no image origin is configured; search/discover use the upstream `/l/` JPEG and detail uses a validated large field.
- Base: `image_direct` is absent or false and existing deployments keep the previous image behavior.
- Good: 713 non-selected Douban candidates upgrade without changing their other-provider current posters, while a matching selected Douban poster follows its upgraded candidate.
- Good: an official large URL returns a 270-pixel-wide image once; the local candidate is accepted and later runs skip the same checked URL.
- Base: the official large URL returns 404 while a local thumbnail exists; retain the thumbnail and retry only after the resolved URL changes.
- Bad: use width alone as completion state, permanently checkpoint a transient failure, or change the thumbnail's provenance to a URL whose bytes were never downloaded.
- Bad: persist a configured CDN URL, use it as the cache key, negative-cache DNS/5xx/non-image failures, route every image through curl, rewrite the Douban JSON API base URL, return `pic.normal`, derive a non-standard thumbnail by guesswork, or delete the small file before the large image is safely stored.

### 6. Tests Required

- Unit-test large-only poster field priority, invalid-field fallback, nested thumbnail rejection, origin validation, `/l/public/` derivation boundaries, known-query removal, unknown-query preservation, and temporary WebP projection.
- Provider/handler-test search and discover thumbnail derivation; assert responses and section cache retain the same official large URL across CDN configuration changes.
- Repository-test keyset candidate projection and CAS behavior for stale candidates, other-provider current selections, and matching Douban current selections.
- Service-test a small or missing candidate through download, DataDir storage, configured origin use, candidate switch, and old-file preservation.
- Assert a successful replacement whose width remains at most 300 is skipped on the second pass; assert usable-small-image 404 checkpointing, missing-file 404 failure, transient retryability, URL-change retry, and stale-candidate CAS rejection.
- Assert the `image_direct` database migration and API update/public/resolve round trip.
- Image-proxy-test live CDN selection, CDN-to-official fallback, official cache identity, exact-404 negative caching, transient retryability, direct-client selection, and cleanup of both failure markers.
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
// Wrong: CDN configuration leaks into provenance and cache identity.
candidate.SourceURL = projectDoubanArtworkURL(officialURL, baseURL)

// Correct: persist the official URL; project CDN only inside a cache miss.
candidate.SourceURL = officialURL
data, err := imageProxy.Fetch(ctx, officialURL)
if err != nil {
	return err
}
```

```go
// Wrong: width alone makes a valid best-available poster download forever.
if candidate.Width <= 300 {
	downloadAgain(candidate)
}

// Correct: durable business state stops only the same completed URL attempt.
if localAvailable && candidate.RepairCheckedURL == resolvedOfficialURL {
	return nil
}
```


## Scenario: Persistent Shared Douban Data

### Scope and storage
- `DoubanSubject` stores original JSONB, independent 0–10 rating, quality,
  `fetched_at`, `last_attempt_at` and `next_retry_at`. Its unique key is
  `(douban_id, entity_kind)`. Confirmed cross-type bindings use
  `MetadataIdentifier.DoubanEntityKind`; empty values retain the local kind.
- Scores and compatibility snapshots follow the current unique identifier.
  Ambiguous associations yield no subject or rating. Movie/Series data tasks
  do not process Season/Episode; legacy own-identity scores remain supported
  without inheritance. General metadata ratings and local artwork stay separate.
- Startup migration chooses complete details before degraded/partial responses,
  then the newest fetching time, in bounded keyset batches. Derive identity from
  the snapshot itself (root, `subject` or `data`), normalize numeric IDs and map
  `tv`/`is_tv` to Series. A current media binding is not required: unbound and
  previously rebound snapshots still belong in the center under their own
  identity. Only missing fields may use a unique valid legacy identifier; when
  the snapshot has an ID, a type fallback must belong to that same normalized ID.
  Reject conflicting identities and unidentified payloads without inventing IDs.
  Validate each candidate before deduplication so an invalid preferred response
  cannot hide a valid one; advance the cursor even for skipped candidates.
  Preserve fetching time, skip existing shared identities and retain old snapshots
  for rollback without changing media bindings. No ongoing legacy Douban writes.
- After migration coverage is verified, an explicitly authorized operational
  cleanup may remove only `metadata_provider_snapshots.provider = 'douban'` rows
  covered by a nonempty shared subject of equal/better quality and no older fetch
  time. Keep the table and every other provider's snapshot. Do not clear local
  canonical fields previously filled from Douban or delete media/ID associations.

### Retrieval and refresh
- The shared Provider serializes cache admission, rechecks persisted state, allows
  canceled waiters to leave and spaces every actual attempt by two seconds.
  This includes permission fallback and existing proxy-route reselection.
  Coordination is per application process; persistent data survives restarts.
- Complete detail TTL defaults to seven days. Existing settings support
  `metadata.douban_detail_ttl_seconds` from 3,600 to 2,592,000 seconds.
  Degraded TTL is capped at one day. Subject-only binding validation is partial
  and cannot satisfy a complete detail request. Regular scraping may reuse its
  historical wrapped abstract response; episode counts require detail capability.
- Normal failures retain previous payload/rating/success time and defer retries
  five minutes; confirmed not-found defers one day. Late failures cannot replace
  newer attempt state. Successful payload writes gate attempt/retry columns
  independently: a late success can fill absent or older eligible details but
  must retain a newer `last_attempt_at` and `next_retry_at`. An accepted success
  at or after the latest attempt clears the old retry state. Complete data cannot
  be overwritten by partial/degraded data or an older success. A downgrade after complete data defers another full
  attempt one day while preserving the complete row.
- `WithDoubanFresh` bypasses TTL and retry backoff for explicit admin operations.
  A failed force request remains a failure. Concurrent arrivals may reuse a
  successful fetching time later than their arrival. Explicit discover refresh
  propagates this flag while retaining section stale/fallback behavior.
- Valid details persist before local fields/poster processing. Compatibility
  writes compare JSON values (JSONB changes whitespace/key order) and never
  extend fetching time merely because local enrichment used cached details.
  `Match.DoubanFetchedAt` carries the original successful fetching time through
  binding, enrichment and regular persistence; copying RawJSON must copy this
  time too. A delayed local write must not overwrite a later subject refresh.
- Search/rail payloads use `DoubanQueryCache`, keyed by normalized URL hash,
  with five-minute search or six-hour rail TTL. Validate expected response shape
  before caching; errors never replace good results. Successful writes remove
  expired query rows. Query responses never become complete subject data.
- Successful subject writes invalidate Web media and Emby item/content caches.
  Display paths remain batched database reads with no per-item network request.
- `douban_data_refresh` / “豆瓣资料刷新” is a separate default-off daily job,
  using settings `metadata.douban_data_refresh_enabled` and
  `metadata.douban_data_refresh_interval_seconds`. It belongs to Common Tasks with kind `douban_data_refresh` and scans due
  Movie/Series rows directly in `douban_subjects`, including unbound subjects.
  It refreshes only shared base details/ratings through the strict Rexxar Movie/TV
  endpoint, with no subject fallback, HTML, celebrities or image requests.
  Supplement age/missing data never selects a fresh base row. Count and selection
  share the same base TTL/quality/retry predicate. Choose one bounded snapshot,
  ordered by oldest `fetched_at NULLS FIRST`, then Douban ID/kind; default cap 100,
  configurable 1–100 using `metadata.douban_data_refresh_count`. Preserve enabled
  and interval keys. A cap is a subject count, not a physical HTTP-attempt cap.
  Same-value success advances base freshness. Admission rechecks cache/backoff;
  a concurrent fresh result, deferred retry or rejected late save is a skip.
  Metrics report total eligible at start, selected, processed, changed, unchanged,
  skipped, failed, remaining eligible at finish and actual HTTP attempts.
  Successful base writes preserve all rich JSON/timestamps/retry and image keys.
  Histories/logs remain independent.
  Startup migrates only legacy `artwork` executions named “豆瓣资料刷新” to
  the new kind/system; the definition key, daily log paths and settings stay stable.
  A temporary upstream failure stops the batch, returns the original error,
  records a failed execution/stage with a sanitized error, and increments
  `failed` and `upstream_paused`; it must not report successful completion.
  The existing enrichment job/key/cursor/history still performs local completion.

### Administrator data center
- `/admin/data-center` and `/api/admin/douban/subjects` belong to administrator
  access; API authorization resolves the current active account and role.
- GET collection supports parameterized literal title substring or exact Douban
  ID search, entity-kind filtering and bounded pagination. Count and rows use
  identical filters; order is `fetched_at DESC NULLS LAST, id DESC`. Return the
  summary projection only, including failed rows, never their complete JSON.
- GET `/:id` reads the persisted shared UUID, exposing parsed details and raw
  JSON without provider requests. Root, `subject` and `data` payloads remain
  readable. A missing row returns 404; invalid parameters/UUID return 400.
- POST `/:id/refresh?mode=base|full` accepts existing Movie/Series rows only.
  Missing mode defaults to `full` for compatibility; empty, unknown or duplicate
  modes return 400. Base forces the strict Rexxar detail reader without fallback
  or supplement changes. Full preserves the existing complete/degraded reader
  and explicit webpage supplementation behavior. Both derive the remote
  identity from storage and uses `WithDoubanFresh`. It never performs canonical
  enrichment, artwork downloads or binding/hierarchy writes. Read the row back
  after success and report the attempt's degraded status separately: quality
  protection may have retained an older complete payload. Upstream failure is
  an error and keeps previous successful data, with persisted retry state.
- No arbitrary URL/new identity, JSON editing, deletion or bulk force-refresh
  is supported. Legacy Season/Episode rows are visible but read-only.

### Verification
- PostgreSQL: `TestDoubanMigrationPreservesIdentityQualityAndTime`,
  `TestDoubanMigrationUsesSnapshotIdentityWithoutBinding`,
  `TestDoubanMigrationPagesPastInvalidCandidates`,
  `TestDoubanSharedSubjectWriteAndAssociation`, cache/refresh tests in
  `internal/service/douban_cache_test.go`, and existing Douban/Emby rating tests.
- Run cache concurrency and cancellation coverage with `-race` and a real
  `MEDIASTATION_TEST_POSTGRES_DSN`; do not count skipped DB tests as validation.
- `TestDoubanPayloadIdentityAtBothLevels` rejects conflicting root/nested IDs
  and kinds while accepting normalized historical numeric IDs.
- `TestDoubanSubjectSuccessPreservesNewerFailure` and
  `TestDoubanSeriesLatePersistencePreservesNewerFailure` cover late successful
  details after a newer failure, retaining retry state and original fetching time.
  `TestDoubanDataRefreshUpstreamFailureIsFailed` covers failed task history,
  batch termination, preserved old details and the five-minute retry delay.
- `TestDoubanManagement*` covers PostgreSQL search/paging/projection, forced
  readback, unchanged canonical data, failure preservation and history guards.
  Administrator route tests cover authentication, role and parameter validation.
  Run `node scripts/check-douban-data-center.mjs` from `web` against an isolated
  Vite server for UI permissions, URL state, cancellation, refresh feedback and
  dark/light responsive layouts; all browser API responses are mocked.

## Scenario: Independent data-center posters

### 1. Scope / Trigger
Data-center image display and shared subject poster persistence, including
subjects with no local media association.

### 2. Signatures
- `douban_subjects.poster_asset_id`: nullable ArtworkAsset reference, SET NULL
  on asset deletion; `poster_source_url`: persisted official large-image URL.
- Summary/detail `poster_url` is `/api/admin/douban/subjects/:id/poster` when
  a payload exists, otherwise empty. GET/HEAD require active administrator auth.
- `DoubanRepository.SetPoster` compares the observed JSONB payload and poster
  fields; `ArtworkRepository.SaveAsset` deduplicates without media selection writes.

### 3. Contracts
List/detail JSON reads perform no download. Image reads parse root/subject/data
through the existing official-large-image rules, reuse an available current
asset or matching local Douban media artwork, then download through ImageProxy
into `DataDir/catalogs/douban/artwork/aa/bb/cc/`. No startup backfill or arbitrary URL
input exists. Persist only valid downloaded images. Subject upserts preserve
poster fields; poster operations never advance freshness/retry timestamps,
change rating/quality, create media metadata or change artwork selections.
Late downloads use CAS; a loser rereads the winner instead of overwriting it.
Use the variant wrapper and controlled storage paths. Current responses use
private/no-cache because the subject UUID is mutable; old-image fallbacks and
image errors use no-store. Existing image negative caching limits failed retries.

### 4. Validation & Error Matrix
Invalid UUID -> 400; missing shared row or missing usable image -> 404;
unavailable services -> 503. Errors do not expose upstream messages. Missing
local original -> repair through the normal image import. Missing large source
or failed download -> preserve old asset and all subject data, serving available
old bytes with no-store. Invalid transform -> existing 400 behavior.

### 5. Good/Base/Bad Cases
Good: an unbound subject's first visible poster is downloaded once and remains
available after the temporary remote cache is gone. Base: an existing matching
media candidate is independently referenced without selecting it. Bad: create
fake metadata ownership, bulk download at startup, cache an old-image fallback
as immutable, or reset successful subject time when a poster is downloaded.

### 6. Tests Required
`TestDoubanPoster*` covers unbound persistence, media reuse/selection isolation,
missing-file repair, source changes, failure state preservation, invalid images,
concurrent deduplication and stale-download CAS on isolated PostgreSQL.
Management route tests cover GET/HEAD auth and UUID validation. Browser checks
serve real mock image bytes and verify loading, placeholders, versions, filters
and responsive themes. Run the Douban/artwork/image-variant tests with race
checking, Web lint/build and `check-image-url.mjs`.

### 7. Wrong vs Correct
Wrong: call `ImportRemote` with a fabricated metadata ID, or return the remote
source URL to the browser. Correct: save a standalone deduplicated asset, CAS
the Douban reference and serve it through the authenticated subject poster route.


## Scenario: Rich administrator subject details

### 1. Scope / Trigger
Administrator subject detail fields, public webpage supplement refresh and cached credit images.

### 2. Signatures
- `douban_subjects.rich_payload` holds public webpage fields and local profile
  keys independently of the unchanged original `payload`. `rich_fetched_at` and
  `rich_next_retry_at` track webpage success and retry independently of API time.
### 3. Contracts
- GET detail only reads storage. Explicit single-subject full refresh
  supplements a successful complete API response with one subject webpage,
  using the same serialized request gate, proxy/Cookie resolution and detail TTL.
  Confirm `og:url`, nonempty heading and info; challenges/wrong pages never
  replace stored rich data. HTML requests accept HTML and cap bodies at 4 MiB.
- A webpage failure preserves stored fields and sets a five-minute supplement
  retry. Scheduled base refresh never examines or retries this supplement state.
  Base-only manual refresh also preserves it. Explicit full refresh reports partial success using
  `supplement_unavailable`; degraded API refresh does not attempt the webpage.
- Rich CAS checks original success time, observed rich JSON and retry state.
  Conflicts merge current same-source avatar keys. New webpage success clears
  supplement retry; avatar writes do not. Compare times at PostgreSQL microsecond
  precision to avoid treating avatar writes as new webpage fetches.
- Profile images resolve only saved credit indices via authenticated admin
  GET/HEAD `/douban/subjects/:id/credits/:index/image`; no arbitrary source URL.
  Official HTTPS doubanio `/view/` sources import lazily through ImageProxy into
  PeopleImageStore's persistent `DataDir/catalogs/douban/people/aa/bb/cc/`; local keys remain in rich data,
  without creating canonical people/media rows. Preserve keys when sources are
  unchanged, CAS references after download, and reject changed-person late
  results. Missing originals repair through ImportCached; errors show placeholders.
  Serve variants with private/no-cache, error no-store, and rich timestamp URL
  versions. HEAD uses the same import/repair behavior as subject posters.
- The wide ModalShell has no extra header/close button or theme toggle; backdrop
  and Escape close, focus returns to its trigger, and global theme applies.
  Display only available metadata, rating count/distribution, awards/honors;
  reuse MediaCredits circular profiles. No trailers/stills or third-party scores.
### 4. Validation & Error Matrix
Invalid subject UUID/index -> 400; missing subject/image -> 404; unavailable
services -> 503. Webpage challenge, wrong identity or malformed page -> preserve
rich data and defer five minutes. Full manual operation reports partial success;
scheduled operation stops the batch. Image errors use no-store and never return
remote source errors to clients.

### 5. Good/Base/Bad Cases
Good: API refresh succeeds and blocked webpage preserves existing credits for a
later retry. Base: a cached subject displays available API fields and local
avatars. Bad: clear rich fields on challenge, create fake canonical people,
perform remote detail requests when opening the dialog, or reset API time when
serving an avatar.

### 6. Tests Required
- `TestDoubanRich*`, management/auth tests, isolated PostgreSQL race
  checks and `check-douban-data-center.mjs` cover identity/failure preservation,
  independent retry, CAS, avatar persistence/repair/HEAD, partial-success feedback,
  profile versions, focus/closing and responsive themes. Real HTML availability
  depends on upstream access and is separate from mocked transport verification.

### 7. Wrong vs Correct
Wrong: compare nanosecond JSON time to a database timestamp and clear failure
retry during an avatar write. Correct: truncate to PostgreSQL microsecond
precision; only a newer webpage success clears supplement retry.

### Bounded base refresh verification
`TestDoubanDataRefresh*`, `TestDoubanBaseRefresh*`,
`TestDoubanManagementBaseRefreshPreservesRichState` and
`TestSchedulerDoubanRefreshCountConfiguration` verify API-only transport,
oldest-first bounded progress, independent rich state, shared TTL, late-save
metrics, admission retries and count persistence. Browser checks
`check-douban-refresh-task.mjs` and `check-douban-data-center.mjs` cover count
settings, task metrics, base/full modes, independent timestamps and access.
