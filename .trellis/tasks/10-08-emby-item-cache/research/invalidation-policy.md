# ITEM 缓存失效条件审计

用户决策：单实例；缓存介质由本次方案决定。选择现有本地 L1 + 可选 Redis L2，重点是响应保持正确和未受影响用户继续命中。

## 失效原则

1. 只根据已提交且可能影响 ITEM 的变化通知；请求成功并不自动代表变更，流程失败也不自动代表没有部分提交。
2. 公共内容 → 全局内容代数；用户状态 → 该用户代数；实际访问范围 → 仓储读版本 + 认证快照。Redis 和本地绑定相同版本。
3. 失效范围必须覆盖所有派生结果。单个 Episode 已看可改变 Series/Season 汇总、未看数、筛选分页和代表版本，因此用户状态先按整用户，不只删详情。
4. 不靠永久缓存或每次全清保证正确；保持 TTL 和容量兜底。外部改动尚未被进程观察时不承诺即时刷新。

## 用户状态：仅失效当前用户

| 条件/入口 | 当前证据 | 建议通知条件 |
| --- | --- | --- |
| Emby SetFavorite | emby_user_data.go:16；成功分支清全 media:emby: | 实际收藏写成功，改清该用户；不支持/目标校验错误不清 |
| Emby MarkPlayed/容器批标 | emby_user_data.go:78；事务及多来源分支 | 提交成功后该用户，包括父容器状态和未看数；空批次且可确认无写不清 |
| Emby RecordProgress | emby_user_data.go:182/:236 | 有效状态保存后该用户；转调 Playback 时确定通知所有者，不能重复 |
| Web RecordProgress | playback.go:78；handler/playback.go:31 | 越过原门槛并保存状态后该用户；85-86 的门槛 no-op 不清 |
| 精确进度快照 | emby_progress_sync.go:69/:86 | CAS 写入事务成功后该用户；冲突/错误不清，不放宽 revision 语义 |
| Web SetFavourite/ToggleFavourite | playback.go:359/:363 | 写成功该用户；幂等 Set 可能改变 media_id，不能只按布尔值判断无变更 |
| 指定媒体历史删除 | playback.go:177 | RowsAffected>0 且提交成功该用户；NFO/黄果清状态与普通删除都覆盖 |
| 批量历史删除 | handler/watch_history.go:118，直接 DB 删除 legacy | 删除成功且 RowsAffected>0 该用户；保留原有只删 legacy 的范围，不顺带改业务 |
| 单条历史删除 | handler/watch_history.go:160，user_id+id 直接删除 | 删除成功且 RowsAffected>0 该用户 |
| 自动补标前集 | RecordProgress 事务内部 | 与本次进度共用一次该用户通知，不能每个 Episode 通知 |

同位置进度仍可能更新 watched_at、revision、代表文件、有效完成状态；相同收藏布尔值可能更新代表 media_id。无可靠 changed 信号时保守通知当前用户，不额外读完整状态做比较。具体来源事务/no-op 输出以工作树接口为准，不为优化而猜测无变化。

## 公共内容：全局失效

| 条件/入口 | 当前证据 | 必要处理 |
| --- | --- | --- |
| 普通媒体创建/编辑/删除、扫描落库/prune | media_cache.go:44；scanner_post_scan.go:7；media_delete.go:17 | 已有 media: 删除包含 ITEM；保留、核对每次提交边界 |
| 来源/NFO 文件绑定变化、版本/Part/path/library 变化 | 列表资格、归属及 MediaSources 消费 media/binding | 在实际入库/重绑定提交点；不只在大任务正常收尾 |
| NFO 手动资料编辑 | nfo_metadata.go:54 已提交后读回 | 提交后通知，即使随后读回失败 |
| 红果 SaveDetail/RebindWork/SaveAlbum | hongguo.go:324；hongguo_album.go:26 | 每个公开内容提交点通知；后续合集失败不能遗漏之前资料 |
| 黄果 SaveDetail/RebindWork | huangguoai.go:220 | 同上；纯重试时间/任务状态不通知 |
| 人物/演职员补齐 | people_backfill.go:139，季分支 :168 | persistCredits 或标识失效等真实公开变更后通知；确认季路径和手动入口 |
| provider 刮削部分成功 | scraper.go:202，先 persistProviderMetadata 后 Update Media | 前序资料提交后，媒体更新失败也通知，不能只保留尾部成功失效 |
| 图片本地修复 | artwork_backfill.go:126 已 defer 通用失效 | 已覆盖；不重复新加清理，后续可仅在有公开变更时通知 |
| 来源图片地址/选择变更 | 来源服务下载/保存图片及展示投影 | 应覆盖提交点；仅图像字节缓存不属于本任务；同 URL 替换图片是否改变 ImageTag 需实施前逐入口确认 |
| Probe 成功存轨道/时长 | media_probe.go:164 删除 media: | 已覆盖；测试异步探测与旧详情回填竞争 |
| DeleteLibraryRoot | media_library_roots.go:236，先删除该 root 媒体，再删 root/同步主路径 | 第一阶段已删媒体便需通知，即使后续失败；不能仅依赖最后 UpdateFields 换代 |
| Add/UpdateLibraryRoot | media_library_roots.go:172/:202 | path/enabled 导致公开内容或读范围变化时通知；仅新增空路径是否影响 ITEM 以实际投影为准，不制造假定数据 |

来源图片/人物头像/字幕编辑所有辅助提交入口仍需实施前列全并验证；本表不把已发现核心路径当作全代码库无遗漏证明。公共内容全局失效是保守方案，先避免遗漏跨库/合集/搜索派生依赖，不构造反向依赖图。

## 权限与库配置

| 条件 | 已有保护 | 新响应缓存要求 |
| --- | --- | --- |
| 用户 hide_adult | user_repository.go:113 更新后 ReadCacheKey 换代 | 响应键必须含此版本和认证快照，否则现有 mediaItems 旧响应仍可命中 |
| 默认 PlayProfile、allow_adult、allowed_library_ids、所属用户、删除 | play_profile_repo.go:18/:59/:73/:78 换代 | 使用仓储版本；事务外分步更新可能部分成功，原换代应保持 |
| adult.enabled、adult.library_ids、emby.library_display | setting_repository.go:30/:41 换代 | 同上 |
| 库名称/type/path/封面等 UpdateFields | library_repository.go:122 换代 | 即使 MediaService 没显式清 ITEM，新的版本键已有保护；不能误报 UpdateLibraryCover 必须另加失效 |
| 账号禁用/删除/密码撤销、跨用户目标权限 | 原每次认证/目标用户检查 | 缓存不绕过认证；非媒体功能权限不加入可见范围键 |

仓储权限代数目前是全局的，因此少量权限/库配置修改仍会令其他用户旧键不命中；本次不另建权限代数体系。高频播放/收藏改为用户代数，收益更直接。

## 不失效的条件

读取；token/设备变化；未改变公开响应的内部任务/重试/分片进度；被门槛忽略的进度；校验失败；事务完整回滚；零行删除。

没有 reliable changed 标志的成功写保守通知对应范围，不能用“响应仍是200”或“输入相同”推定无需失效。

## 关键验证

- A 写播放/收藏后 A 新读刷新，B 暖缓存仍命中；公共内容变更后 A/B 都刷新。
- 用户容器状态、未看数、筛选列表和原代表版本同步正确。
- 短进度/零行删除/完整回滚不换代；同值但变化 watched_at/media_id 的写仍失效。
- 旧读完成只写旧版本；Redis 失效失败/L1 存在/context取消不使新请求读旧值。
- 分阶段资料、删 root、人物补齐即使后半段失败，也对已提交变化通知。

未运行测试/数据库/Redis；上述结论来自代码定位与关键工作树片段点验。
