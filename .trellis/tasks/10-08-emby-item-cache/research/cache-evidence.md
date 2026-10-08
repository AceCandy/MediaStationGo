# 缓存设计证据

本记录基于 CodeGraph 定位与工作树原文；没有修改产品代码或执行运行时验证。

## 关键证据

| 事实 | 位置 |
| --- | --- |
| Items 外层负责前瞻裁切及收藏归属，内层分发多个来源 | internal/service/emby_compat.go:128、:168 |
| mediaItems 是现有页缓存入口 | internal/service/emby_items_list.go:17、:98 |
| 前缀 media:emby:，IDs 键排序且字段用分隔符连接 | internal/service/emby_items_cache.go:11、:23、:30 |
| 原子仓储读版本支持旧并发读只能填旧键 | internal/repository/read_cache.go:9、:26 |
| visibilityCache 已使用仓储版本及认证 hideAdult 快照 | internal/service/emby_visibility.go:54 |
| 用户 hide_adult、默认播放配置、成人设置、库基础写推进读版本 | internal/repository/user_repository.go:113；play_profile_repo.go:18、:60；setting_repository.go:30；library_repository.go:122 |
| Web 功能权限表不同于 Emby 可见库；后者来自默认 PlayProfile 和成人设置 | internal/service/visibility.go:58；internal/service/permission.go:112 |
| media: 删除已覆盖 ITEM；探测成功也通知 | internal/service/media_cache.go:44；scanner_post_scan.go:7；scraper_local_metadata.go:228；media_probe.go:164 |
| Web 有直接 PlaybackService 进度和收藏入口，无运行时缓存注入 | internal/handler/playback.go:31、:66；internal/service/playback.go:359；service_builder.go:127 |
| NFO 编辑提交后直接读回，没有通用失效 | internal/service/nfo_metadata.go:54 |
| 来源详情、合集和绑定有独立提交，需要逐提交点通知 | internal/service/hongguo.go:324；hongguo_album.go:26；huangguoai.go:220 |
| 详情触发异步 track probe | internal/service/emby_playback.go:111、:125 |
| token 在 HTTP 层服务返回之后递归注入 | internal/handler/emby_items_handlers.go:67、:106、:115；emby_playback.go:114 |
| 服务播放路径不是上游真实 STRM 目标 | internal/service/emby_playback.go:259、:361 |
| PostgreSQL 未设测试 DSN 会 Skip，fixture 有隔离 schema | internal/testdb/postgres.go:24 |

## 独立探索结果复核

外围探索曾将 ITEM 前缀误写为 emby:items:，据此报告普通媒体失效未覆盖 ITEM。这与工作树常量 media:emby: 不符；已纠正，不采纳该缺口判断。其“权限保存未清 visibility”也不能直接推出权限缓存失效：须区分 Web 功能权限和实际可见范围，并追到 repository.ReadCacheKey 的成功写换代。

同类审计规则：先点验命名空间常量，再判断父前缀包含关系；对内存权限缓存先检查版本键及写仓储换代，不能仅因上层服务没有 DeletePrefix 就断定遗漏。

## 尚需实施阶段验证

所有来源图片/绑定辅助提交点需列完整清单；本文仅列已确认核心入口。JSON 缓存后的内部 Items 类型断言、部分成功通知、Redis 故障、竞争时序及暖读查询边界必须用实际回归验证。

当前无生产耗时、命中率或多实例运行证据；用户已确认单实例，失效条件修订见 invalidation-policy.md。
