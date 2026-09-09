# Design

追加批准范围：收藏使用依赖当前收藏作品的 LATERAL UNION ALL 展开本体、季、集，再关联文件，保留原窗口排序和详情加载。季复查将相关 EXISTS 改成文件元数据 ID 与有文件分集父 ID 的集合 membership；OFFSET 0 保留媒体扫描边界，避免逐集索引探测。该集合仍每页计算，不宣称恒定成本。

人物候选在仓库过滤中文和当前语言/版本/上下文的负缓存，以现有 ID 顺序分页，只加载必要上下文。中文范围严格沿用 containsChinese。1000 是组数不是行数，保留人物优先和 season 角色上下文；到达上限仍需补齐已选择组的全部 targets，不能在分页边界截断。

豆瓣利用 metadata_id/provider 唯一约束，用一次 LEFT JOIN 替换重复快照 EXISTS。保持 SQL NULL、degraded、fetched_at、JSON、GROUP BY/HAVING 和海报资产存在性语义，不假定 SQL 文本条件顺序就是实际执行顺序。

风险为 Unicode 范围漂移、负缓存误匹配、跨页角色遗漏和 JOIN 空值差异，用 PostgreSQL 回归覆盖。无接口、配置变更。回滚只涉及本任务 diff，不覆盖其它改动。

用户已批准增加角色部分索引迁移：复用 ensurePerformanceIndexes，以 CREATE INDEX IF NOT EXISTS 创建 metadata_credits(metadata_id,id)，谓词仅为 original_role 非空、role=original_role、原角色不含 U+4E00–U+9FFF。不限定参数化的角色类型，以兼容通用预编译计划；不把无上限角色文本放入索引键。首次启动构建会增加启动时间并阻塞该表并发写入，后续幂等。只在隔离库执行验证，不直接给实际库建索引。

重启后实测旧索引仍扫描 112025 条 Writer/Director，用户批准把键调整为 (type,metadata_id,id)。使用新名称 idx_metadata_credits_type_pending_translation，先创建成功再删除旧索引，避免 IF NOT EXISTS 跳过已有定义。部分谓词不变；测试加入大量非演员英文角色，防止仅验证索引名称却遗漏低选择性扫描。
