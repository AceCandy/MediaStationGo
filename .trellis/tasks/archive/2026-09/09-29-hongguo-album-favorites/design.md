# 红果合集收藏设计

## Decision and boundaries

新增独立用户收藏表 `hongguo_favorites(user_id,item_id,favorite,updated_at)`。合集的 item_id 直接使用现有 `hg-group-<官方合集ID>`；电影及尚未补齐合集的独立作品使用稳定 source_id。播放状态继续使用源作品及集号，收藏不再借用 episode_number=0。

在 repository 统一定义收藏身份表达式和单行读写。Web 整剧入口与 Emby 入口通过目标作品/合集的索引范围检查是否有可见文件，然后读写一个收藏关系。不为收藏生成完整节点、播放状态、图片或统计。已有 movie/source API 按当前作品身份转入相同收藏表。

## Read consumers

- `HongGuoSeriesFavorite`、`HongGuoRepository.UserState(...,0)`、`SetFavorite`。
- `HongGuoRepository.UserCards(favourites)`、`SearchCandidates`。
- Emby hierarchy/detail、library page payload、global work/batch candidates、global hierarchy candidates、resume favorite filter。
- Web“我的收藏”实际使用 `hongguoAPI.userCards`，无需改界面。API 返回形状保持原样。

## Compatibility

启动迁移在一个事务内把原 episode=0 收藏合并到新身份（同用户同合集 BOOL_OR，保留最新更新时间），再移除已迁移的旧 episode=0 行；其它分集播放状态和事件不变。迁移重复执行安全，已有新记录优先，不能复活已取消收藏。

尚未分配合集的源作品收藏在 `SaveAlbum` 首次获得关系时转入合集；已收藏的合集不随某个成员改组而移动。合集加入新季无需复制收藏。没有可见文件时列表不展示，但收藏关系保留。

已有合集关系的电影转为剧集时，在详情更新事务内同样提升源收藏；已有目标合集状态优先，不能覆盖明确取消。源入口与合集补齐共用作品行锁。收藏列表先使用 source_id/related_album_id 原生索引限定成员，再校验统一收藏身份。

## Files and risks

### 全局收藏列表性能补充

YAMBy 的准确计数请求暴露了遗漏路径：工作候选先全量汇总红果合集，外层才筛收藏，导致估算膨胀和昂贵 JIT。`hongGuoWorkMembers` 在 IsFavorite 请求中先通过用户收藏与 source_id/related_album_id 原生索引定位成员；`hongGuoWorkScope` 的标题/时间聚合只读已收藏合集，但仍保留该合集的全部有效成员。收藏的文件日期排序改为目标成员内聚合。非收藏、普通及 NFO 分支、数据库配置和响应格式保持不变。

生产源文件仅需调整 `emby_hongguo_library.go`；相关收藏/全局 oracle 与 60 万文件计划测试覆盖准确计数和取页。不开缓存、不新增表或迁移、不部署重启。

模型注册和数据库兼容迁移、红果收藏仓库与上述所有 SQL 消费者、相关 PostgreSQL 回归与执行计划测试、Emby 接口目录说明、红果规范。无需新依赖或通用收藏框架。

迁移应通过正常部署启动执行，本任务不运行生产迁移、不重放用户收藏请求、不重启运行服务。数据库升级后降级需恢复升级前备份或显式反向迁移，不能仅回退二进制。
