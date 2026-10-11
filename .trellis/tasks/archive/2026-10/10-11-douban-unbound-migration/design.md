# 技术设计

根因在启动迁移：INNER JOIN 当前豆瓣关联，以关联身份过滤和去重，遗漏了无关联或改绑后的历史资料。

以旧豆瓣快照为扫描起点，SQL 提取根层、subject、data 的 ID 与实际类型，tv 映射 series，必要时使用 is_tv。LEFT JOIN LATERAL 仅提供唯一合法旧关联，用于缺失字段；快照已提供 ID 时，缺失类型仅能从同一个规范化 ID 的关联补足，防止改绑后的 ID 与另一条目的类型混用。数字 ID 去除前导零，零及非法 ID 跳过。复用模型多层身份校验，拒绝冲突响应。

按 complete、degraded、partial 和 fetched_at 倒序、快照主键升序分页，每批 1000 条。游标跟随每条候选推进；不提前 DISTINCT ON，以免无效优先候选遮挡有效资料。NOT EXISTS 比较解析后的身份，ON CONFLICT DO NOTHING 保护已有行及并发写入。只插入共享资料，不改变旧行、媒体关联、刷新调度或管理页面。

改动文件：internal/database/douban_migration.go（根因修复）、douban_migration_test.go（回归）、豆瓣持久资料规范（独立身份验收）。隔离 PostgreSQL 16 先复现，再验证迁移及相关豆瓣回归、竞态检查和 vet，之后独立只读复核。真实数据库仅 SELECT，3 条无法识别记录的处理另待确认。
