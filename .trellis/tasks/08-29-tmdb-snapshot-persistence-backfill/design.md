# TMDB 快照全链路写入与一次性历史回填技术设计

## 1. 设计摘要

复用现有 TMDB provider、provider snapshot repository、settings 和任务中心，不新增表、依赖或周期调度器：

```text
正常刮削 / 手动匹配 / 目录补全
              │
              ▼
       TMDB 完整详情 RawJSON
              │
              ▼
UpsertProviderSnapshot(metadata_id, "tmdb")

首次升级启动 / 任务中心手动重试
              │
              ▼
有 TMDB identity 且无 snapshot 的 metadata
              │  Movie / Series / Season / Episode 串行处理
              ▼
       同一 snapshot upsert
              │
              ├─ TaskExecution metrics / 脱敏日志
              └─ 完整枚举结束后写一次性完成标记
```

## 2. 前向快照写入

- Movie/Series：在共享 canonical 持久化成功后，如果接受的 TMDB match 带合法 `RawJSON`，直接调用现有 `UpsertProviderSnapshot`。
- 搜索候选后补详情：复用已返回完整 match 和 `RawJSON` 的 `GetMovieMatch` / `GetTVMatch`，避免继续使用会丢弃原始 JSON 的薄详情结果。
- Season/Episode：在对应 canonical metadata 已成功保存后，复用现有 Season/Episode 详情结构中的 `RawJSON` 写入。
- 手工编辑 TMDB ID：先按 metadata kind 获取完整详情并验证 RawJSON；在同一受控流程中写 identifier 和 snapshot，任一步失败均保留原 identifier/snapshot，不留下半成功状态。
- Catalog hydration 保持现有写入，不建立第二套 snapshot helper；如确有完全相同的三行调用，仅抽取一个局部私有函数。
- JSON 为空或非法时不写；snapshot 失败按现有主匹配的 best-effort 语义处理，不回滚已成功的 metadata 匹配。

## 3. 历史候选与 provider 请求

### 3.1 候选查询

在现有 metadata repository 增加：

- 一次候选总数查询，用于任务初始 `total`。
- 按 `metadata_items.id` keyset 分页的候选查询，条件固定为：kind 是 Movie/Series/Season/Episode、存在同 kind 的合法 TMDB identifier、`NOT EXISTS` TMDB provider snapshot。
- 候选同时返回标题、kind、TMDB ID 和 Season/Episode 请求所需的 Series TMDB ID、季号、集号；层级不完整的行仍进入候选并记录失败，不能静默漏掉。

查询不连接 `media`，所以未关联本地媒体但具有 TMDB identity 的 metadata 也会覆盖。

### 3.2 执行模型

- 固定小批 keyset 分页，provider 请求串行执行，复用现有 TMDB client 的超时和限流行为。
- 每条请求成功后立即 upsert snapshot；单条失败只增加 `failed` 并继续。
- 每页重新按“无 snapshot”条件取数。进程中断后从空 keyset 重新开始也只会看到仍缺失的记录，无需新增游标表。
- Movie/Series 按自身 TMDB ID 获取完整详情；Season/Episode 复用现有按 Series ID 和层级坐标获取详情的方法。
- 历史任务只写 snapshot，不调用 metadata 投影字段或 artwork 持久化。

## 4. 一次性状态

- 使用内部 setting（建议键名 `internal.tmdb_snapshot_backfill_completed`）记录一次性迁移是否完成，不新增 schema。
- `Container.Boot()` 在任务恢复完成后检查该键：未完成才异步启动；已完成则保持静默。
- 只有候选枚举到末尾才写完成标记。进程取消或致命 repository 错误不写，下次启动继续。
- 单项 provider 失败不妨碍枚举完成和写完成标记；失败项留在任务 metrics/日志中，后续只由管理员手动重试。
- 手动重试不清除也不改写一次性标记，只处理当时仍缺 snapshot 的候选。
- 自动与手动执行使用独立 task kind 或等价的专用互斥，保证同一进程最多运行一个 TMDB 快照回填。

## 5. 任务中心契约

- 新增稳定定义：key `tmdb_snapshot_backfill`，名称“TMDB 快照回填”，触发方式“一次性自动 / 手动”，server-owned action 使用现有 `POST /api/tasks/definitions/:key/run`。
- 不注册 scheduler job，不提供 enabled/interval 配置。
- 自动执行使用 event 或现有合适的自动 trigger；手动执行使用 manual trigger。两者进入同一任务定义、历史和按日日志。
- metrics 固定为：
  - `processed`：本次实际处理数；
  - `total`：启动时缺失候选总数；
  - `succeeded`：成功写入数；
  - `failed`：处理失败数；
  - `remaining`：本轮初始候选中尚未检查的数量，运行中为 `max(total - processed, 0)`，完整枚举后为 0；仍缺 snapshot 的已检查项由 `failed` 表示。
- 任务页定义行通用读取 `latest.metrics` 或活动任务 metrics，显示“已处理 / 总数、成功、失败、剩余”；继续复用现有 3 秒刷新，不新增 API。
- provider 错误进入日志前使用现有 URL/查询参数脱敏，不写 API key、完整 URL 或请求参数。

## 6. 结束状态与异常矩阵

| 情况 | 快照 | 一次性标记 | 任务结果 |
| --- | --- | --- | --- |
| 无候选，完整枚举结束 | 不变 | 写入 | completed，数字均为 0 |
| 单项 provider 失败 | 该项保持缺失 | 整轮结束后写入 | 最终 failed，保留 metrics，其他项继续 |
| snapshot upsert 失败 | 该项保持缺失 | 整轮结束后写入 | 最终 failed，保留 metrics，其他项继续 |
| repository 分页/计数致命失败 | 已成功项保留 | 不写 | failed；下次启动续跑 |
| 进程取消/退出 | 已成功项保留 | 不写 | interrupted；下次启动续跑 |
| 完成后服务重启 | 不访问 provider | 保持 | 不创建新执行 |
| 管理员手动重试 | 只处理仍缺失项 | 保持 | 新增一次执行历史 |

## 7. 兼容与回滚

- 不修改 provider snapshot schema、provider 三态值域、现有详情字段或图片选择。
- 保持现有保留语义：删除 media、媒体库或库根不删除 metadata、identifier 或 snapshot；metadata merge 继续迁移/去重 source snapshot。
- 代码回滚不会删除已补快照；内部完成 setting 可保留，不影响旧版本。
- 若需要重新执行，优先使用任务中心手动动作，不提供清空完成标记的 UI。
- 任务完成后的唯一长期行为变化是：新的 TMDB 详情会同步保存原始快照。
