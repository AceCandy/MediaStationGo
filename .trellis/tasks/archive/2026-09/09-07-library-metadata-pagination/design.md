# Design

用户已确认 Web 与 Emby 同时修改。作品分页由数据库完成，文件仅用于归属、可见性、排序、计数及必要的操作身份。

- Web 在 MediaViewRepository 增加库内逻辑 metadata 分页和轻量统计；重用已有 metadata 展示投影。电影卡片与整剧卡片保留具体操作文件 ID，另以 metadata ID 驱动作品身份。整剧使用 canonical key，旧 key 仅在显式深链解析时兼容。
- Web 列表 handler 在服务调用前传递分页与目标过滤；分集加载限定目标作品，避免进入一部剧时扫描全库。
- Emby seriesMetadataPage 保留其 SQL ID 分页与排序，改用按页 SQL 统计，不再物化分集。轻量统计不得写入完整整剧/季缓存。
- Emby 混合库先合并逻辑 ID 和排序值，在数据库统一分页，再构造当前页 payload。
- Emby 子级浏览使用独立的分页查询，详情与 PlaybackInfo 的完整版本读取维持原语义。
- 保持字段、权限、收藏、播放及管理入口；数据库操作为只读查询，不增加迁移或修改媒体文件。

风险集中在排序口径、分段/版本计数、跨库可见性和操作 ID。用 PostgreSQL fixtures 覆盖这些边界；禁止把未运行的数据库测试视为通过。
