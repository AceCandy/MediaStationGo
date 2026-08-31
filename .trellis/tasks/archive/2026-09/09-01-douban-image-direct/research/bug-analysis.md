# Bug Analysis: configured Douban mirror skipped curl fallback

## 1. Root Cause Category

- **Category**: B / E — Cross-layer contract plus implicit assumption.
- **Specific Cause**: artwork projection replaced the official Douban host with a configured mirror, while image transport still treated the literal `doubanio.com` host as the provider identity.

## 2. Why Earlier Fixes Failed

1. Adding a direct Go client improved one transport path but retained the official-host-only curl gate.
2. Making curl available for official hosts did not cover URLs after `base_url` projection.
3. Browser and local-repair behavior looked separate, although both ultimately used `ImageProxy` and needed one shared policy.

## 3. Prevention Mechanisms

| Priority | Mechanism | Specific Action | Status |
| --- | --- | --- | --- |
| P0 | Architecture | Resolve provider-scoped image policy before selecting transport; do not recover provider identity from a rewritten host alone. | Done |
| P0 | Test coverage | Assert official/configured hosts, unrelated hosts, direct-only clients, curl eligibility, and mode-specific failure markers. | Done |
| P1 | Documentation | Record `image_direct` API, migration, runtime, cache, and error contracts in the Douban spec. | Done |

## 4. Systematic Expansion

- **Similar Issues**: any provider that rewrites an upstream URL to a configurable CDN or mirror can lose fixed-host transport behavior.
- **Design Improvement**: keep provider-scoped decisions at the shared transport boundary while matching only the provider's official or currently configured host.
- **Process Improvement**: trace URL projection through display, prefetch, download, fallback, and negative-cache paths before changing one caller.

## 5. Knowledge Capture

- [x] Updated `.trellis/spec/backend/douban-cookie-config.md`.
- [x] Added focused configuration and image-proxy regressions.
- [x] No template copy exists in this repository to synchronize.
