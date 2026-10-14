# 技术设计

## 边界
复用 TMDbDataCenter 的请求/持久租约/预算及原季集复查队列；不同实体由独立任务选择候选。周期补缺不进入 explicit_pending 工作者。来源完整性与本地下载状态分开。沿用季集坐标、404、租约和并发身份校验。

豆瓣复用 Rexxar 基础 API 与共享准入锁。评分保存有独立时间及重试状态，评分请求不推进正文时间、不覆写正文。资料补全与评分通过共享锁重新核验，避免并发重复。

新增小型持久媒体同步队列，由来源/绑定/媒体入库变化事务登记，仅读来源投影与已有资产；manual 跳过、版本条件写入。失败可重试，来源变更不丢失。手动同步可登记现有绑定媒体，自动模式只处理待办。

## 调度和兼容
新任务 key 带 tmdb_data_/douban_data_ 前缀，资料中心拥有图片和季集维护。旧任务执行历史保留，旧可运行定义退出。独立配置未设置时默认关闭，避免拆分扩大旧网络预算；不清除旧配置，便于回滚。媒体只展示媒体资料同步。旧季集待办查询兼容保留并在新任务关联入口显示。

## 核验依据
internal/service/task_definitions.go；internal/service/tmdb_data_center.go；internal/service/tmdb_recheck_queue.go；internal/repository/tmdb_recheck_timing.go；internal/service/douban_cache.go；internal/repository/douban_repository.go。

## 审查修复边界
目录读取在资料中心使用独立 inventory 请求身份和 payload；已有条目只保存目录/活跃字段，正文补全和手动刷新仍走普通组件。
媒体图片同步只使用本地可读资产，旧同来源引用失效时按观察快照条件替换；其他来源、manual 和可用旧图保持不变。
季集状态查询按输入坐标选择来源，避免单目标重复读整剧；不改变缺项、身份、冷却与租约判断。
