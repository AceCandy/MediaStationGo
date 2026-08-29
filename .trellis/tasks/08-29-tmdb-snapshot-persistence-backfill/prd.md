# TMDB 快照全链路写入与历史回填

## Goal

让所有成功获取的 TMDB 详情都保存原始 JSON 快照，新数据不再漏写，历史缺失数据可以受控、可重试地补齐。

## Background

- 当前普通自动刮削和手动匹配会保存 TMDB ID、投影字段和图片，但不保存 TMDB 原始详情快照。
- 当前手工编辑元数据时也可以直接替换 TMDB ID；`MediaService.UpdateMetadata` 只写 identifier，不请求 TMDB 详情，因此是“有 TMDB ID、无快照”的现存前向入口。
- 只有发现目录补全 `catalog hydration` 稳定写入 TMDB 快照，导致普通媒体与发现目录行为不一致。
- 当前删除媒体、媒体库或库根只删除 media，不删除其 canonical metadata、provider identifier 或 provider snapshot；本任务保持该保留语义。
- 2026-08-29 运行库中有 4,325 个带 TMDB ID 的 Movie，4,314 个有 TMDB 图片，仅 345 个有 TMDB 快照；全类型合计 3,981 条缺快照，其中 3,971 条同时已有 TMDB 图片。
- 示例《时间之子》创建于 2026-08-07，早于 TMDB 快照体系；但现行普通刮削仍无法为它回填快照。

## Requirements

- R1：普通自动刮削、手动匹配、手工编辑 TMDB ID 和目录补全在成功获取 TMDB 详情后，统一保存当次原始 JSON 快照。
- R2：Movie、Series、Season 和 Episode 的 TMDB 详情快照行为一致，不因数据是从媒体库还是发现目录进入而不同。
- R3：快照按 `(metadata_id, provider)` 幂等 upsert，只写入合法 JSON，重复刮削更新 `payload` 与 `fetched_at`，不制造重复记录。
- R4：TMDB 详情获取失败时不写入伪快照，也不因可选详情失败破坏已成功的媒体匹配。
- R5：历史回填只处理“有合法 TMDB identifier、无 TMDB snapshot”的记录，使用有界批次、串行或受控并发、失败隔离和重试，不一次性突发请求 TMDB。
- R6：历史回填仅补快照，不覆盖现有标题、简介、图片选择或手工修改的元数据。
- R7：复用现有 TMDB provider、snapshot repository 和后台任务机制，不新增依赖，不修改快照表 schema。
- R8：现有 provider 三态语义保持不变；回填成功后，有 TMDB 图片的记录可从 `missing` 变为 `complete`。
- R9：升级后仅自动在后台执行一轮历史回填，范围是全库所有 Movie、Series、Season 和 Episode 中“存在合法 TMDB identifier 且不存在 TMDB snapshot”的 metadata；不限于已关联媒体的记录。完整执行结束后，后续服务启动不再自动扫描；只有进程中断时才续跑未完成的这一轮。
- R10：自动回填必须在任务中心以独立稳定任务“TMDB 快照回填”展示，保留执行历史和按日志，并支持管理员手动重试。
- R11：任务中心列表直接展示 `已处理 / 总数 / 成功 / 失败 / 剩余`，通过现有任务 metrics 实时更新，不要求管理员只靠日志判断进度；“剩余”表示本轮尚未检查的候选，完整枚举后为 0，失败项单独计入“失败”。
- R12：单条记录以数据库中 snapshot 的存在性作为成功检查点；一次性自动回填的执行状态持久化记录。进程中断后仅续跑仍缺失的记录；一轮完整结束后即使有失败项也不再自动重跑，失败项由管理员在任务中心手动重试。
- R13：手工编辑 TMDB ID 时必须先按 metadata kind 获取合法完整详情并写入 snapshot；详情请求或 snapshot 写入失败时拒绝保存新 ID，避免产生新的“有 TMDB ID、无快照”记录。
- R14：TMDB snapshot 和 canonical metadata 不因关联媒体、媒体库或库根被删除而删除；本任务不新增无媒体 metadata 的自动清理或快照清理。

## Acceptance Criteria

- [ ] AC1：自动刮削成功的 TMDB Movie/Series 都有对应的有效 TMDB snapshot。
- [ ] AC2：手动 TMDB 匹配成功后产生同样的 snapshot，不需要再访问发现页。
- [ ] AC2a：手工编辑 TMDB ID 成功后同时存在合法 snapshot；TMDB 详情请求或 snapshot 写入失败时，原 identifier 和 snapshot 保持不变。
- [ ] AC3：Series、Season 和 Episode 现有 catalog hydration 快照行为不退化。
- [ ] AC4：相同 metadata/provider 重复写入仍只有一条 snapshot，并更新获取时间。
- [ ] AC5：详情请求失败时不产生无效 snapshot，已接受的媒体匹配仍保留。
- [ ] AC6：历史回填中断后可继续且不重复处理已成功记录；完整结束后重启服务不会再次自动扫描，单项失败不阻断其他记录。
- [ ] AC7：回填《时间之子》后，数据库存在 TMDB snapshot，原有 TMDB 图片和元数据不被覆盖。
- [ ] AC8：相关 Go 单元测试、静态检查与 `git diff --check` 通过。
- [ ] AC9：当前运行库的 3,981 条缺失记录都在自动回填候选范围内，其中 3,980 条关联本地媒体、1 条未关联媒体。
- [ ] AC10：升级后的首次启动会创建一次“TMDB 快照回填”执行并持久化状态；执行完成后服务重启不再自动创建该任务，管理员手动执行仍会记录新的运行结果，包括空结果。
- [ ] AC11：任务中心运行中列表每 3 秒刷新并显示数字进度；执行结束后 `剩余=0`，保留最终成功/失败 metrics 与脱敏日志；存在失败项时执行状态为 failed，管理员可手动重试。
- [ ] AC12：一条记录失败时其他记录继续处理；失败记录保持无 snapshot，只在任务中心手动重试时再次处理，日志不暴露 API key、请求 URL 或查询参数。
- [ ] AC13：删除媒体、媒体库或库根后，原 metadata、TMDB identifier 和 TMDB snapshot 仍保留；元数据合并继续按现有规则迁移或去重 snapshot。

## Out of Scope

- 更改 provider 状态值域或详情页展示。
- 修改现有封面/背景图选择、优先级或本地文件。
- 为其他 provider 新增快照体系。
- 注册周期回填任务，或在一次性迁移完整结束后的每次服务启动重新扫描。
- 定期重新刷新已存在的 TMDB snapshot；本任务只补缺失快照，新刮削或手工 ID 编辑负责更新它自己获取的快照。
