# Technical design

权威设计：`docs/huangguoai-design.md`（全文，已根据用户最终批准更新）。本任务采用独立 `internal/huangguoai` 协议客户端、`huangguoai_*` model/repository/service/handler，复用公共 Media/MediaProbe、任务、配置、权限、流处理及必要安全工具；不重构红果为插件。

资料→发现/详情与下载；下载→本地校验无覆盖发布→整理/扫描→独立绑定→MediaView/Web/Emby→独立用户状态。来源权限与成人内容检查贯彻直接 API 和图片。追加迁移与枚举分派，旧源无数据转换；关闭采集仍可读取历史文件。

源码接入锚点：`model.AllModels`、`repository.New`、`newServiceContainer/Boot/Close`、`registerAuthedMediaRoutes`；扫描 `buildLocalScanMedia`、MediaView 来源投影、Emby Item/Items/global候选/续播及 PlaybackStates。实现前以真实工作树核对具体文件。

异常边界：无法确认分类或跨类型冲突不以页顺序/集数决定类型；电影真实多分集保留资料且暂不擅自发布额外分集。已确认类型不受计数或完结更新影响。回退优先停用来源，不自动删表/历史/文件；裸旧程序降级兼容须另验。
