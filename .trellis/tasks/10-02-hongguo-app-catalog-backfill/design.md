# Design

移除增量循环强制 page=1，让既有 NextPage 推进。本轮仍使用开始时的旧边界判断追平，失败后沿既有 NextPage 从已提交页继续恢复。

新增同包 AppCatalog 扫描，固定 HTTPS 分类接口，单分类扫描复用随机设备身份与 session，UseNumber 与来源 ID 去重，严格校验 has_more/offset，沿用项目普通 App 签名。匿名身份仅内存。

新增独立仓储 SaveAppDiscoveryPage：事务内插入摘要，冲突仅补空摘要；根据既有 discovery/work 分类优先补空值，非空不覆盖。新增作品进入现有 pending 资料队列，命令不启动媒体下载或自动详情刷新。逐页提交保证取消/失败保留之前结果，重复扫描幂等。

cmd/hongguo-backfill 默认仅扫描；--apply 使用已有配置写库，不自动迁移，不修改官网同步游标。一个命令使用信号取消并有总时限。执行时复用运行实例的有效配置，已完成实际补录；不保存或输出连接凭据。
