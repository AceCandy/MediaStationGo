# 统一继续观看的剧集聚合：设计

## Boundary

只修改继续观看的读取与投影，不修改播放进度写入、历史唯一键或数据表。完整历史继续以单集 `metadata_id` 为身份。

## Data Flow

```text
逐集播放历史 / 红果用户状态
          ↓ 过滤未完成、门槛、用户与可见性
          ↓ 解析带来源边界的逻辑剧集键
          ↓ 每组选择 watched_at 最新记录
          ↓ 按 watched_at 倒序并分页
          ↓ Web Continue / Emby Resume / IsResumable
```

## Group Identity

- 本地 Episode：使用元数据父链投影得到的 `SeriesID`。
- 本地 Movie 或无有效 `SeriesID` 的项目：使用自身逻辑 `MetadataID`。
- 红果 Episode：使用红果源作品身份；红果 Movie 使用自身作品身份。
- 所有键包含目录来源命名空间，禁止使用标题，也禁止跨本地与红果合并。

## Contracts

- 只在 `completed=false` 的继续观看查询中启用聚合。
- 每组选择 `watched_at` 最新记录；确定性并列顺序沿用现有稳定 ID/时间顺序。
- 聚合必须先于 `limit` / `StartIndex`，可见性必须先于最终返回。
- Web API DTO 与 Emby payload 不增加字段、不改变已有 Item ID。
- 复用一处剧集身份/选择规则；各入口只保留其现有 envelope 和投影逻辑。
- Emby 接口行为变化同步管理员接口目录说明，但不新增接口。

## Compatibility and Rollback

- 无 schema migration，无历史数据重写；回滚代码即可恢复旧行为。
- 完整历史与单集 UserData 不变，Series/Season 已播放派生规则不变。
- 红果状态继续使用独立表和可见性规则。

## Risks

- 若在分页后内存去重，会造成返回数量不足；必须在查询分页前聚合。
- 若使用扫描提示 `Media.SeriesID` 或标题，会错误合并；必须使用权威元数据父链/红果源作品身份。
- `/Items/Resume` 与 `Filters=IsResumable` 是两条实现链路，测试必须同时覆盖。
