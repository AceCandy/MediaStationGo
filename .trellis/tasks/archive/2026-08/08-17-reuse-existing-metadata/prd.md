# 复用已有元数据跳过重复刮削

## Goal

当待刮削媒体已经关联到一条能够被当前精确 Provider 标识重新解析出的 canonical metadata 时，直接恢复为 `matched`，避免重复请求 Provider 和重复更新 `metadata_items`。

## Background

- 当前刮削入口只在 `media.metadata_id` 为空时执行精确 metadata 解析并提前返回。
- `media.metadata_id` 已存在但 `scrape_status=pending` 时，会跳过该快捷路径并继续请求 Provider，即使当前 Provider ID、实体类型和剧集层级仍解析到同一条 metadata。
- 扫描本身不直接更新 `metadata_items`；重复 Provider 刮削会执行 canonical upsert，造成无意义的 `updated_at` 变化与搜索索引重建。

## Requirements

- 对非强制刮削，使用媒体当前的 TMDB、Bangumi、豆瓣、TVDB 标识以及实体类型执行精确解析。
- 已有关联且精确解析结果等于当前 `media.metadata_id` 时：
  - 设置 `scrape_status=matched`；
  - 清空 `scrape_error` 和已消费的 `local_metadata_hint`；
  - 不调用任何外部 Provider；
  - 不写入 `metadata_items`、`metadata_identifiers`、图片或演职员数据。
- 电影按 Provider、`movie`、外部 ID 验证；剧集文件必须按 Series Provider ID、季号、集号解析到当前 Episode metadata。
- 新剧集入库时，已有 Series 和 Season 只用于定位，不因新增 Episode 重复刮削或更新；Episode 是独立匹配单元。
- Episode 记录不存在时必须继续刮削该集；Episode 记录存在但完整性不足时也必须继续刮削该集。
- Episode 记录只有在标题不是默认占位标题（例如 `第 1 集`、`Episode 1`）或该记录最近 7 天内已更新时，才视为可直接复用；标题仍是默认占位标题且 `metadata_items.updated_at` 已超过 7 天时，必须重新刮削该集。
- 精确解析为空、解析到不同 metadata、标识冲突或剧集层级不完整时，保留现有 Provider 刮削流程。
- `IncludeMatched` 或 `RefreshWeakMatched` 明确请求刷新时，不使用该快捷路径。
- 保留 `metadata_id` 为空时现有的精确绑定快捷路径。

## Acceptance Criteria

- [ ] 已关联且为 `pending` 的电影，当前 Provider 标识解析到相同 metadata 时直接变为 `matched`，Provider 调用次数为 0。
- [ ] 上述快捷匹配不改变对应 `metadata_items.updated_at`，也不执行 canonical upsert。
- [ ] 已关联 Episode 只有在 Series、Season、Episode 层级解析到同一 Episode metadata 时才直接匹配。
- [ ] 新 Episode 不会因为已有 Series/Season 而被错误标记为 matched；已有 Episode 才能进入直接匹配判断。
- [ ] Episode 缺失或被判定为信息不完整时继续单集刮削。
- [ ] 标题为 `第 X 集`/`Episode X` 等默认占位标题且 `updated_at` 早于 7 天的 Episode 会重新刮削。
- [ ] 同样的默认标题但 `updated_at` 在最近 7 天内时可直接匹配，避免短时间重复请求。
- [ ] 当前标识解析到其他 metadata、无法解析或标识冲突时，继续现有刮削流程，不静默保留可能过期的关联。
- [ ] `IncludeMatched` 和 `RefreshWeakMatched` 仍能执行明确请求的 Provider 刷新。
- [ ] 原有“metadata_id 为空但精确标识命中”的直接绑定行为保持正常。

## Out of Scope

- 不改变 canonical metadata 的字段合并规则。
- 不处理 canonical upsert 内容相同时的通用幂等优化；本任务从刮削入口避免已可复用 metadata 的重复 upsert。
- 不改变 Catalog hydration、人物回填、本地化或手动元数据编辑行为。
