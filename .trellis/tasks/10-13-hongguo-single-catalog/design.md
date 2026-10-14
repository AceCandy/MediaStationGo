# 设计

HongGuoWork 新增 summary_only（默认 false 保持已有详情语义）；摘要写入显式 true，SaveDetail 原位升级 false。RefreshedAt 保留详情刷新语义，摘要使用零时间。SourceID 唯一约束保证身份。摘要不写分集/人物；图片继续独立表并保留详情图片优先。分类页只更新摘要字段，可给详情填空分类；榜单只更新摘要标题并填空分类；App 摘要只填空；搜索只插缺失。

列表直接读取作品表，取消 UNION/反连接；hydrated 为 NOT summary_only。FindBySourceID 保持详情语义；补齐、合集、周期更新、绑定、下载按状态过滤。

一次性 SQL 在停服务并备份后执行：锁表→添加状态→仅插入缺失摘要、填空分类→校验覆盖→删除旧摘要表。旧详情 UUID/关联不动，不添加启动兼容逻辑。回滚需恢复备份及旧版本。
