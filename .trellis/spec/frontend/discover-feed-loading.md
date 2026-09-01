# Discover Feed Loading Contract

## 1. Scope / Trigger

Use this contract when changing `DiscoverPage`, `discoverAPI.feed`, the discover feed handler, section caching, or discover poster loading. It prevents repeat Provider calls, stale-request UI writes, and page-wide image cache busting.

## 2. Signatures

```ts
discoverAPI.feed(
  sectionKeys: string[],
  page?: number,
  refresh?: boolean,
  signal?: AbortSignal,
): Promise<DiscoverFeedResult>
```

```http
GET /api/discover/feed?sections=<comma-separated keys>&page=<positive integer>&refresh=1
```

```go
loadDiscoverSections(parent context.Context, svc *service.Container, keys []string, page int, refresh bool) []discoverSectionResult
```

## 3. Contracts

- Missing `refresh`, or any value other than `1`, may return an existing non-empty section cache entry within the service's six-hour TTL without calling the Provider.
- `refresh=1` bypasses only the pre-request cache fast path. Provider failure still resolves in this order: section cache -> configured Provider fallback -> per-section error.
- Responses keep `items[sectionKey]` and `_meta[sectionKey]`; metadata includes `page` and `has_next`, with `stale`, `fallback`, `warning`, `error`, or `disabled` only when applicable.
- Same-Provider jobs remain serial; different Provider groups use the existing bounded worker count. Cache hits do not alter input ordering.
- Douban cache entries, cloned responses, and preheat inputs keep the same official large poster URL. The image proxy applies the current configured CDN only during an image-cache miss, so changing that configuration never mutates feed URLs or requires section-cache invalidation.
- The Web page groups sections by page for initial load and refresh. A row page change requests only that section and target page.
- Starting a new feed batch aborts the previous controller. Aborted batches must not update rows, errors, loading state, or localStorage cache.
- Discover poster URLs stay stable across mounts and manual data refresh. Use native `loading="lazy"`; only a failed individual image retry may add `v=r1` through `v=r3` and `refresh=1`.

## 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| `page < 1` or invalid | Normalize to page `1`. |
| Unknown section key | Drop it without failing other sections. |
| Ordinary request with valid non-empty cache | Return cached items with their stable official Douban poster URLs; do not call Provider. |
| Ordinary request with empty or expired cache | Call Provider through existing scheduling. |
| `refresh=1`, Provider succeeds | Return fresh items and replace the section cache when non-empty. |
| `refresh=1`, Provider fails, cache exists | Return cache with `stale=true`, correct `has_next`, and do not call fallback. |
| Provider fails without cache | Try configured fallback, then return a per-section error. |
| Provider succeeds with empty items | Return empty items and do not cache them. |
| Browser aborts an old batch | Do not display a request error or clear the active batch's loading state. |

## 5. Good / Base / Bad Cases

- Good: a hot page visit reuses section cache, keeps poster URLs unchanged, and transfers cached images without page-wide version parameters.
- Base: a cold cache calls Providers with the existing concurrency limits and fills both service and localStorage caches.
- Bad: changing one row page re-fetches unchanged rows, or refresh adds a timestamp/version to every poster URL.

## 6. Tests Required

- Handler: cache hit returns the keyed response and does not call Provider.
- Handler: mixed cache hit/miss preserves order and still loads misses.
- Handler: `refresh=1` calls Provider, returns fresh data, and a following ordinary request reuses that data.
- Handler: refresh failure with cache asserts items, `stale=true`, `has_next`, and absence of `fallback`.
- Handler: update the Douban image CDN between two cache-hit requests; assert both responses and the stored cache keep the same official poster URL.
- Web checks: lint and build pass; browser verification observes one section on row pagination, `refresh=1` on manual refresh, an actual abort during rapid actions, stable poster URLs, and `r1`-`r3` only on failed-image retry.

## 7. Wrong vs Correct

### Wrong

```ts
setImageVersion(String(Date.now()))
discoverAPI.feed(allSelectedSections, changedRowPage)
```

This forces every poster to use a new URL and re-fetches rows whose page did not change.

### Correct

```ts
activeController.abort()
discoverAPI.feed([changedSection], changedRowPage, false, nextController.signal)
```

Keep ordinary poster URLs stable and use `refresh=1` only for explicit recommendation-data refresh or an individual failed-image retry.
