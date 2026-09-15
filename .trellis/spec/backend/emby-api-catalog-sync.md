# Emby API Catalog Synchronization

## 1. Scope / Trigger

Apply this contract whenever an Emby-facing route or handler changes in a way visible to a player. A catalog sync is required in the same task when any of these change:

- HTTP method, canonical path, `/emby` prefix availability, case variant, or route alias
- public versus token-authenticated access
- token carrier support, parameter name/source/requirement, or request content type
- response status, content type, top-level shape, important player-consumed field, redirect, stream, or HEAD behavior
- implementation level: real business behavior versus fixed/empty compatibility response

The backend route and handler remain the authority. The static catalog is a checked projection for administrators, not an independent API definition.

## 2. Signatures

Authoritative backend registration and behavior:

```text
internal/handler/emby_routes.go
internal/handler/emby_routes_lowercase.go
internal/handler/emby_*.go
internal/middleware/emby_auth.go
internal/service/emby_*.go
```

Frontend projection:

```ts
export const EMBY_API_ENDPOINTS: readonly EmbyApiEndpoint[]

type EmbyApiEndpoint = {
  id: string
  category: EmbyApiCategory
  methods: readonly EmbyApiMethod[]
  path: string
  aliases?: readonly string[]
  auth: 'public' | 'token'
  support: 'implemented' | 'compatibility'
  parameters: readonly EmbyApiParameter[]
  responses: readonly EmbyApiResponse[]
}
```

The catalog owner is `web/src/pages/embyApiCatalog.ts`; the renderer is `web/src/pages/AdminEmbyAPIsPage.tsx` at `/admin/emby/interfaces`.

## 3. Contracts

- Add one catalog entry per semantic request/response behavior. Merge paths into `aliases` only when method, authentication, parameter contract, handler behavior, and response shape are equivalent.
- Describe the canonical registered path without `/emby`. The page-level rule covers routes registered under both the empty prefix and `/emby`.
- Write prefix-specific routes literally. For example, `/emby/api/stream/:id` must not imply that `/api/stream/:id` exists.
- Include uppercase, lowercase, and user-scoped variants that real Emby clients call. Do not duplicate a full entry solely for casing.
- `auth: 'token'` means a valid token is required. Do not mark `X-Emby-Token` itself as required when the middleware also accepts Authorization or query carriers.
- Use `support: 'implemented'` only when the handler performs real lookup, playback, state mutation, or session behavior. Fixed or empty probes use `support: 'compatibility'`.
- Response fields and examples must match the current JSON layer consumed by the player. Different top-level shapes cannot be aliases even when both responses are empty.
- Date-valued item fields such as `PremiereDate` and `DateCreated` are UTC strings formatted as `2006-01-02T15:04:05.0000000Z`; do not place a `time.Time` directly in an Emby response map.
- Binary, redirect, HEAD, subtitle, and no-content behavior must state their actual status/content type instead of presenting a JSON example.
- Examples use fictitious IDs, hosts, usernames, and tokens. Never include local media paths, real account data, signed URLs, or secrets.

## 4. Validation & Error Matrix

| Backend change | Required catalog action | Failure prevented |
|---|---|---|
| Add or remove a route | Add/remove the entry or alias | Advertising a missing endpoint or hiding a supported one |
| Change route casing/prefix variants | Update aliases and prefix notes | Player path mismatch |
| Change auth middleware/token carriers | Update `auth`, parameters, and auth summary | Claiming a header is mandatory when another carrier is valid |
| Change request parsing | Update parameter location, type, requirement, and example | Invalid integration requests |
| Change response/status/content type | Update response fields and examples | Client parsing based on stale documentation |
| Replace fixed response with real behavior, or vice versa | Change `support` | Misrepresenting compatibility probes as complete features |
| Change stream/STRM handling | Update 200/206/302/404/502 and HEAD notes | Incorrect playback expectations |

## 5. Good / Base / Bad Cases

- Good: adding a lowercase SearchHints route updates the existing SearchHints `aliases` because it uses the same handler and shape.
- Base: internal refactoring with identical externally observable route, request, and response behavior requires an explicit catalog comparison but no catalog edit.
- Bad: grouping `/Items/:id/ThemeMedia` under the fixed empty Items response; ThemeMedia has a different top-level response contract.
- Bad: marking `X-Emby-Token` as a required header while the middleware also accepts Bearer, MediaBrowser, and query tokens.
- Bad: adding `/api/stream/:id` as an alias for `/emby/api/stream/:id`; the unprefixed alias is not registered.

## 6. Tests Required

For every triggered change:

1. Compare changed registrations in `emby_routes.go` and `emby_routes_lowercase.go` with `EMBY_API_ENDPOINTS`.
2. Read the concrete handler/service before changing parameters, statuses, fields, or support level; route registration alone is insufficient.
3. Confirm catalog IDs are unique and every changed semantic endpoint has a response description.
4. Run `cd web && npm run lint`, `cd web && npm run build`, and `git diff --check`.
5. Browser-check `/admin/emby/interfaces` at 390x844, 768x1024, and 1440x900 with no document-level horizontal overflow.
6. Exercise search, category filtering, empty results, detail expansion, and copy failure/success feedback.
7. Run a main-content accessibility audit in both light and dark themes, and verify direct non-admin access remains denied.

## 7. Wrong vs Correct

### Wrong

```ts
{
  path: '/emby/api/stream/:id',
  aliases: ['/api/stream/:id'],
  auth: 'token',
  parameters: [{ name: 'X-Emby-Token', location: 'header', required: true }],
}
```

This advertises an unregistered path and treats one optional token carrier as mandatory.

### Correct

```ts
{
  path: '/emby/api/stream/:id',
  auth: 'token',
  parameters: [{
    name: 'X-Emby-Token',
    location: 'header',
    description: 'Recommended; Authorization and supported query token carriers are also valid.',
  }],
}
```
