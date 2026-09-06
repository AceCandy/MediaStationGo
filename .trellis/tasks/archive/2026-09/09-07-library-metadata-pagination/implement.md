# Implementation

1. 实现 Web metadata 分页与当前页统计，连接电影/整剧列表 handler；验证筛选、总数、身份及空库。
2. 限定 Web 选集查询到目标 metadata，核对详情、收藏、版本和管理操作。
3. 实现 Emby 整剧摘要、混合库统一分页与子级分页；比较播放器响应与接口目录。
4. 添加 PostgreSQL 回归，断言逻辑分页与不读取全量分集；运行相关 service/handler/repository 测试。
5. 运行 Web lint/build、已有相关脚本及 git diff --check，独立复核 diff。
6. 记录验证结果及未验证风险，不启动或遗留调试服务，不自行提交。

回滚只需恢复本次列表查询、投影与调用方改动；没有数据库迁移。
