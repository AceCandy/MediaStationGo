# 技术设计

## 边界

本任务只改变本地文件扫描指纹、媒体数据库删除语义和回收站产品入口。共享元数据与用户状态仍以 `MetadataID` 为所有者，媒体硬删除不清理这些记录，也不删除磁盘文件。

## 扫描指纹

在 `Media` 上增加两个仅供扫描使用的持久化字段：

- `scan_file_size_bytes bigint`：扫描路径本身的大小；对 `.strm` 指边车文本文件，不是播放目标。
- `scan_file_mtime_ns bigint`：扫描路径 `os.FileInfo.ModTime().UnixNano()`。

不能复用 `media.size_bytes`：本地 `.strm` 探测后该字段保存播放目标大小，用它比较边车会造成每次扫描抖动。

`walkInfo` 携带大小和纳秒 mtime，扫描构造 `Media`、现有媒体快照及 Upsert 更新映射都传递上述字段。已有记录仅当扫描指纹已建立、大小和 mtime 相同、并且现有本地元数据与路径派生信息无需刷新时才跳过。跳过发生在 Upsert 和 probe 排队之前，因此 `media.updated_at`、探测字段及 `media_probe_metadata.probed_at` 均不变化。

旧记录的 `scan_file_mtime_ns=0` 视为未建立指纹：升级后的首次扫描处理一次并写入当前指纹；第二次未变化扫描跳过。文件大小或 mtime 任一变化都重新读取 `.strm` 目标并按现有流程探测。

## 硬删除

保留共享 `Base.DeletedAt` 和 `media.deleted_at` 兼容列，避免为无运行价值的删列拆分基础模型。所有实际媒体删除调用显式使用 `Unscoped().Delete`，确保不会写入删除时间：

- `DELETE /media/:id` 后台手动删除；
- 扫描 `RemovePath` 与缺失路径批量清理；
- 重复媒体与整理流程的清理；
- repository 按库、按根目录删除路径。

启动迁移幂等地永久删除历史 `media.deleted_at IS NOT NULL` 行。现有 `media_probe_metadata.media_id -> media.id ON DELETE CASCADE` 负责清理探测文档。共享元数据以及仅保存普通 `media_id` 字符串的播放历史、播放事件、收藏、播放列表项和 STRM 记录保持不变。

## 回收站退役

删除回收站 service、handler、单条恢复/彻底删除路由、批量路由、scheduler job/task definition、Web API、页面、路由和导航入口。现有删除按钮改用 `mediaAPI.delete`，文案明确“永久删除数据库记录，不删除磁盘文件”，并保留确认步骤。

## STRM 自动生成边界

扫描结束后的自动 STRM 生成只处理非 STRM 源媒体；源路径或容器已经是 `.strm` 时在任何文件写入前跳过，避免生成嵌套 STRM 或改写源文件 mtime。手动 STRM 导出仍允许把远程 STRM 转换为本站播放 URL，不改变现有导出用途。

## 兼容与迁移

- `AutoMigrate` 新增扫描指纹列。
- 兼容迁移仅删除历史软删除媒体，不删除 `media.deleted_at` 列。
- 迁移重复执行无变化；硬删除失败时启动迁移失败，避免半完成状态被静默忽略。
- 保留既有 media active partial indexes；所有存量与新行的 `deleted_at` 均为 NULL，因此查询语义不变。

## 风险与回滚

- 文件系统或复制工具可能保留 mtime；用户已指定以“大小 + mtime”为变化依据，因此同大小同 mtime 的内容替换不会被发现。手动重新探测仍可用于修复播放探测数据。
- 历史回收站记录的迁移删除不可恢复；这是已确认行为。
- 回滚代码不会恢复已硬删除媒体或已清理的历史回收站记录；数据库备份是唯一数据级回滚手段。
