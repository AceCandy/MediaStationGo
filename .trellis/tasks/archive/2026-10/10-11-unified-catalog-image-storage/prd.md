# 统一图片资源目录与历史迁移

## Goal

将应用管理的持久图片统一到 catalogs/体系/artwork或people/三级内容哈希目录，兼容历史引用并明确跨来源去重和缓存边界

## Requirements

- R1: Application-managed persistent originals use `DataDir/catalogs/<system>/<artwork|people>/aa/bb/cc/<full-content-hash>.<ext>`. Remove the `sha256` directory segment and retain SHA-256 content identity.
- R2: Cover the existing shared artwork/person stores and Hongguo/Huangguo catalog images, including Douban center images. Include historical files and references; preserve API authentication, image variants and current image selection.
- R3: Uploaded library covers use `catalogs/local/artwork`; clearing or replacing a cover changes its reference without deleting images still referenced elsewhere.
- R4: Keep original images in user media libraries intact. Proxy caches and bundled Web assets retain their respective cache/build lifecycles. Reuse existing managed-image variants by copying matching legacy versions to the new cache identity, including already-migrated originals; keep old caches for recovery.
- R5: Each source owns its files; deduplicate within source/category and allow identical content across sources (approved by the user). Do not guess a historical provider from a content hash.
- R6: Catalog thumbnails live beside their original in `<hash>.thumbnails/<algorithm-version>/<spec>`, and remain reusable after moving the data directory or changing file timestamps. Automatically reuse valid existing thumbnails from both historical cache layouts without deleting old files. Remote proxy and non-catalog image caches retain their lifecycle.

## Acceptance Criteria

- [x] New managed originals use the agreed source directory and three hash shards, without a `sha256` directory.
- [ ] Historical image references still serve the correct bytes through unchanged authenticated URLs after migration and restart. Isolated migration and serving tests passed; deployed restart remains unverified.
- [x] Repeated/interrupted migration is safe; missing files are counted and ambiguous ownership uses `legacy`, without deleting data.
- [x] Same-content reuse follows the agreed source ownership policy; changing or clearing a library cover cannot break another reference.
- [x] Existing images, variants, HEAD/ETag, repair, failure fallback and source-file protections remain covered by focused tests.

## Confirmed evidence

- `internal/service/image_asset_storage.go:21` generates the shared legacy key. `storedImagePath` at line 49 accepts safe relative paths.
- `internal/model/metadata.go:78` makes artwork SHA and storage key globally unique; `internal/repository/artwork_repository.go:108` reuses assets by SHA. One asset can therefore be referenced by different sources.
- `internal/service/artwork_store.go:37` and `internal/service/people_image_store.go:35` use shared `artwork`/`people` roots. Avatar integrity checks at `people_image_store.go:167` compare against the exact generated key.
- `internal/service/hongguo.go:54` and `internal/service/huangguoai.go:46` already use source-owned catalog artwork roots.
- `internal/service/media_library_cover.go:27` stores uploaded covers separately; lines 86–123 show per-library clearing and legacy paths.
- `internal/service/image_variants.go:36` and `internal/service/image_proxy.go:66` define rebuildable cache roots. Variants mirror managed source paths.

## Approved decisions

- Source-owned persistent files with deduplication within each source and image category; identical bytes may exist across sources. The user accepted separate copies after discussing cross-source shared posters.
- Uploaded library covers and local/manual imports use `local`; historical assets without an attributable reference use `legacy` rather than guessing TMDb/Douban.
