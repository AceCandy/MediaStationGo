# 当前实现证据

## 详情响应

- `internal/repository/media_view_repository.go:13-35`：`mediaViewSelect` 从当前 canonical metadata identifier 投影 TMDb、豆瓣 ID，并投影 `metadata_kind`；扫描提示不是详情展示来源。
- `internal/model/media_view.go:7-69`：`MediaView` 已承载上述 canonical 字段，可增加只读详情字段而不改 schema。
- `web/src/pages/MediaDetailMetadata.tsx:18-73`：标题下已有可换行的元数据徽章区域，适合放置外部 ID 链接。

## 豆瓣补齐

- `internal/repository/metadata_repository.go:411-433`：现有候选 SQL 只选没有豆瓣快照的唯一豆瓣 Movie ID，因此空快照会永久排除作品。
- `internal/service/douban_enrichment.go:57-65`：已有快照时直接解析旧 payload，不发起 provider 请求。
- `internal/service/douban_enrichment.go:75-98`：现有流程保存快照、fill-only 字段并保存 poster 候选；选择存在时不覆盖。
- `internal/service/douban_enrichment.go:113-160`：可填字段为标题/原名/简介/上映日期/语言/地区/类型/评分/年份，写入前持有行锁并重新判断空值。
- `internal/service/douban_enrichment.go:164-229`：任务每批 20 条、keyset 游标、串行延迟、单项失败隔离；当前所有非失败非跳过结果都计作 `enriched`。

## 设计约束

- 核心缺失条件由用户确认：有效 poster 缺失、简介为空或标题不含汉字。
- `metadata_provider_snapshots.fetched_at` 只在完整成功路径末尾更新，作为 24 小时冷却时间；任何持久化失败不推进冷却。
