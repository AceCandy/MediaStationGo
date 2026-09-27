# Emby 请求错误修复

## 目标

修复请求日志已确认的播放参数解析失败、空剧集查询、取消误报及缺少时长的进度越界。

## 验收标准

- PlaybackInfo 接受 TranscodingProfiles.MinSegments 的整数及整数字符串，空值沿用现有兼容规则视为未指定，非法数字仍返回 400。
- Shows 季列表必须提供非空剧集 ID；分集列表必须提供非空剧集或 SeasonId；无有效父级时返回 400，不执行媒体查询。保留 SeasonId 优先规则及大小写路由。
- Emby 浏览 handler 的请求取消返回 499，真实内部错误仍返回 500。
- Emby 未上报时长而使用当前媒体探测时长时，将位置裁剪到该时长；覆盖普通、NFO、红果。客户端明确上报的非法边界仍拒绝，Web 校验保持不变。
- 对应回归测试使用隔离 PostgreSQL schema，验证状态写入及非法请求不写入；同步接口目录和规范。
- Movie 响应省略 SeriesId、SeriesName、SeasonId、SeasonName、ParentIndexNumber、IndexNumber，普通及 NFO 的详情、列表、续播共用该规则；Episode 保留原层级字段及特别篇的零季号，电影 ParentId 和播放源不变。
- Movie 搜索提示不得重新补出 IndexNumber、ParentIndexNumber 的 null 值；非电影提示字段保持原状。

## 边界

修改请求模型、Emby 浏览 handler、Emby 进度服务、共享条目响应及其测试、接口说明和相关规范。不增加依赖、容差配置或数据库迁移；不调整查询性能、不重启现有服务。电影响应字段修正已获用户批准；是否能消除 Hills/Yamby 空 ID 请求仍须实机验证，不将推断记录为已确认的客户端根因。

## 验证

先运行回归测试确认失败，再实施并运行相关 Go 测试、前端 lint/build、接口目录检查及独立代码复核。
