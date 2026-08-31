# Douban Cookie Configuration Contract

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
