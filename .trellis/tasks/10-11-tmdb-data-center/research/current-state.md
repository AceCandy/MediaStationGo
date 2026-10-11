# 当前实现证据与设计约束

本轮仅静态读取项目与既有任务文档；不查询生产业务数据、不改变数据库、不重启服务。

## 存储

- `internal/model/metadata.go:25`：本地 `MetadataItem` 已有 movie/series/season/episode；季按 parent+season_num、集按 parent+episode_num 唯一。
- `internal/model/metadata.go:67`：`metadata_identifiers` 唯一 `(provider,entity_kind,external_id)`，一个 TMDb 身份最多关联一个本地 metadata。
- `internal/model/metadata.go:78`：`artwork_assets` 按 storage_key 唯一；SHA 不再全局唯一。`metadata_artworks` 是本地作品选图，不等于 TMDb 来源选图。
- `internal/model/metadata.go:121`：`metadata_provider_snapshots` 按 metadata_id+provider 唯一，保存 payload/degraded/fetched_at，删除 metadata 会级联删除快照。无法独立表达多语言、多组件、摘要与扩展快照的新鲜度。
- `internal/model/person.go:11`：people 保存公共身份、显示字段和头像；person_identifiers 按 provider+external_id 唯一；metadata_credits 保存作品关系及原文/译文角色。
- `internal/repository/person_repository.go:387`：来源 ID 优先匹配人物，可恢复软删除人物；无来源 ID 才使用 local 姓名匹配。没有已确认的人物自动合并机制。
- `internal/repository/catalog_repository.go:187`：快照统一入口对豆瓣已转向独立中心；TMDb 仍写旧快照。
- `internal/repository/catalog_repository.go:204`：同一 episode ID 的整季派生摘要不能覆盖已有 extended 快照。
- `internal/repository/catalog_repository.go:218,232`：改绑与快照在同事务；补空快照前检查当前 metadata/identifier，保护并发改绑。
- `internal/repository/metadata_repository_merge.go:11`：本地作品合并迁移关联后删除源；旧快照目标优先，不适合直接作为新中心的来源质量裁决规则。

## 调用和任务

- `internal/service/tmdb_match.go:13,107`：movie/tv 已获取中文详情和 credits/external_ids/translations 等附加组件；返回 RawJSON。
- `internal/service/tmdb_catalog.go:13`、`tmdb_episode.go:13`：季、集详情已存在。季响应可提供 episode inventory。
- `internal/service/tmdb_season_batch.go:35,57`：执行内按 base/key/series/season 合并请求，最多 32 季/16 MiB；不是跨任务持久资料缓存。
- `internal/service/tmdb.go:144,174`：HTTP 429 最多四次尝试，Retry-After 当前最多等待30秒；其他上游失败并非通用自动重试。新中心必须共享冷却并将每次实际 HTTP 尝试纳入预算。
- `internal/service/discover.go:53`：TMDb 榜单返回上游图片URL。
- `internal/service/discover_detail.go:22`：本地命中用本地作品展示，未命中才访问远端；GET 不写本地 metadata。
- `internal/service/metadata_tmdb_refresh.go:17`：现有手动刷新会更新本地作品、图片和关系，不能直接当成只更新中心的刷新操作。
- `internal/service/tmdb_snapshot_backfill.go:80`：旧快照补缺已是任务，完成标志和仍有缺口共同决定启动行为；不可照抄早期归档设计中“完成标志即永久停止”的旧描述。
- `internal/service/task_definitions.go:84,89,90,92`：已有 tmdb_snapshot_backfill、tmdb_artwork_local_repair、tmdb_artwork_missing_recheck、tmdb_episode_metadata_recheck。区分稳定 definition key 与底层共享 task kind/name，不宣称现有任务都使用独立 kind。
- `internal/service/artwork_store.go:45,56`：远程原图导入先用 ImageProxy.Fetch，在 cache/images 落一份，再保存正式资产；中心图片可通过直接下载/按来源映射复用消除此中间副本。
- `internal/service/people_image_store.go:68`：人物图片已有直接抓取再持久化路径，可复用下载机制，但中心资料刷新不能改公共 people 的手工头像。
- `internal/service/image_variants.go:151`：资料图片缩略图现已用 `<hash>.thumbnails/v1/<spec>`；原图迁移已独立完成，TMDb中心不重复搬图片。

## 页面与兼容

- `web/src/appRoutes.tsx:239,282`：资料中心在 admin-root 下，继承 adminOnly；导航放观看空间，地址仍为 `/admin/data-center`。第一版维持此权限。
- `web/src/api/douban.ts:53`：豆瓣中心列表、详情、base/full刷新已有管理员接口。
- `web/src/pages/DiscoverDetailModal.tsx:35`：管理员打开本地命中的TMDb详情会自动POST刷新本地资料。设计明确取消这种隐式本地写入，改成中心 ensure/显式本地刷新。
- `.trellis/spec/frontend/discover-feed-loading.md`：发现权限、按页缓存、取消、ownership按可见媒体查询；新中心不能让资料记录冒充已入库媒体。
- `.trellis/spec/backend/shared-media-metadata.md` 的 Season-Batched 章节：现有本地单集演员列表读所属季，禁止复制季关系到每集。中心可保存来源单集 guest/crew，但不改此本地契约。

## 外部API验证边界

当前代码没有已实现的 TMDb 人物详情/人物履历接口。尝试读取官方 person-details/person-combined-credits/search-person 文档时环境返回 HTTPError，未取得可信响应样本。设计中的 person details、combined credits、person search 是拟接入能力，实施首步需对照官方 schema/样本做 contract test；不得把本轮标为接口已联调。电视剧聚合演职员接口未纳入第一版必需项，现有 credits 与单集 guest/crew 已足以定义无递归的首版范围。
