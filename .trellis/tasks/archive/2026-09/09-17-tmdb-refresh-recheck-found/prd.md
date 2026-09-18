# 手动刷新解除季集未收录状态

成功保存手动刷新的 TMDB 季／集信息后，对应条目立即退出“上游未收录”筛选。季响应中成功保存的已有单集也适用；资料缺失不应继续表示上游不存在。

验收：按 metadata_id 主键更新既有待办，不全量扫描、不新增待办；解除旧错误及冷却，转普通待办，交给现有流程判断完整性；刷新失败或季清单未包含的单集保留原状态；并发旧领取不能回写旧结果。使用 PostgreSQL 回归测试验证，不修改媒体文件或部署数据。

验证结果：临时 PostgreSQL 上服务、仓储的 TestRefreshMetadataTMDb、TestTMDbRecheck、TestMergeTMDbMetadata 全部通过；已独立复核定点更新和旧凭据拒绝。额外 TestDiscoverTMDbRefreshCooldown 因其测试模型未包含 metadata_credits 表失败，未改动该无关测试。未部署、未做生产数据回填或浏览器验证。
