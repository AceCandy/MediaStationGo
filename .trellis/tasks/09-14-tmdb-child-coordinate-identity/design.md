# 技术设计

## 边界

只修改自动季/集复查的 Season 详情读取规则。请求、持久化事务、identifier 唯一约束、手动刷新和其他补全流程均复用现有实现。

## 数据流

1. `TMDbRecheckState` 提供唯一整剧 TMDb ID、本地季号和可选旧季 ID。
2. `fetchTMDbMetadataRecheck` 使用整剧 ID 与季号请求详情，仅校验返回季号及通用响应有效性，不比较旧季 ID。
3. `persistTMDbMetadataRecheck` 在现有受租约/快照保护的事务内调用 `ReplaceIdentifierWithSnapshot`，原子替换季 ID 与快照。
4. 数据库唯一约束继续阻止返回 ID 被两个同类 metadata 同时占用。

## 取舍

- 选择坐标作为 Season 自动修复权威，与 Episode 现有行为一致，并解决历史 ID 过期后的永久重试。
- 保留 child TMDb ID，用于 provider 快照、查询和冲突保护。
- 不把此语义扩展到手动身份刷新；手动操作明确按当前 child ID 刷新，继续严格校验可避免无意改绑。

## 回滚

恢复 Season 分支对 `candidate.TMDbID` 与响应 ID 的比较即可；无数据库迁移或数据格式变化。
