# Existing image integration findings

- ArtworkStore owns DataDir/artwork; prepareStoredImage uses SHA256 storage keys,
  validates bytes and dimensions and writes atomically. ImageProxy owns download
  rules, remote request coalescing and negative caching.
- Existing SaveSelection/SaveCandidate paths can change media selections. A
  standalone subject requires a pure SaveAsset operation and a separate foreign
  key; never fabricate metadata ownership.
- doubanPosterURL accepts cover.image.large.url then pic.large; ResolveArtworkURL
  persists the official large image rather than configured CDN projections.
- ArtworkStore Serve wraps ImageProxy.serveImageFile. Its variant helper previously
  hardcoded immutable cache policy; forwarding the caller policy is required for
  old-poster no-store fallback and mutable subject URLs.
- Local reuse is scoped to matching metadata identifier/kind and exact official
  source URL, using existing metadata indexes rather than scanning every asset.
- The current artwork service worker handles only /api/img, so it does not capture
  the authenticated subject poster endpoint.
