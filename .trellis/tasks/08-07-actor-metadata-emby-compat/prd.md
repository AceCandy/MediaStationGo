# 演员信息与 Emby 人物兼容

## Goal

为电影、剧集和单集建立可共享、可持久化的演职员信息，使新刮削或重新刮削的媒体在 Emby 客户端中显示演员、客串、导演、编剧、角色和头像，并可进入人物列表及人物详情。

## Background

- `MetadataItem` 当前没有演员或人物关联；TMDb `Match` 也没有 cast 数据。
- 本地 NFO 已解析 `<actor><name>/<role>`，但 `LocalMetadata` 不保留结构化演员。
- Emby 模型已经定义 `EmbyItem.People` 和 `EmbyPerson`，实际 item payload 未填充 `People`。
- `/Persons` 和 `/persons` 当前绑定空列表处理器。
- 现有手动媒体库刮削会设置 `IncludeMatched=true`，可作为既有媒体的显式回填入口。
- Emby 原生 People 类型包含 `Actor`、`GuestStar`、`Director`、`Writer`、`Producer` 和 `Composer`。
- 本项目现有 Douban、Bangumi、TheTVDB provider 均未解析演职员；可验证的可靠输入只有 TMDb credits 和本地 NFO。
- 本项目已有数据库配置驱动的 OpenAI-compatible `AIService`，但尚无演职员批量翻译方法或独立开关。

## Requirements

### R1. 共享人物身份

- 新增共享 `Person`，至少保存姓名、规范化姓名、简介和头像来源 URL。
- 新增 `PersonIdentifier`，以 `(provider, external_id)` 唯一标识 TMDb 人物；provider 和 ID 在 repository 边界规范化。
- TMDb 人物必须按 TMDb person ID 复用，不能仅按姓名自动合并。
- 无稳定人物 ID 的 NFO 演员按规范化姓名复用本地人物；不得与 TMDb 人物仅凭同名自动合并。
- 人物和标识均遵循项目现有软删除及恢复写入模式。

### R2. 作品演职员关联

- 新增 `MetadataCredit`，关联 `MetadataItem` 和 `Person`，保存 Emby credit type、原始角色/职务、中文展示角色/职务和稳定顺序。
- 本期支持 `Actor`、`GuestStar`、`Director`、`Writer`；同一人物可在同一作品中拥有不同类型关系。
- 同一人物在同一作品中允许多个不同角色；重复导入同一快照必须幂等。
- TMDb movie cast 关联电影 metadata；TMDb TV cast 关联 Series metadata。
- 本地电影 NFO cast 关联电影；本地单集 NFO cast 关联 Episode。
- Episode 按人物类型合并自身与 Series credit：自身存在某类型时使用自身该类型，否则继承 Series 同类型；Episode 客串不会导致 Series 主演丢失。
- metadata 合并必须迁移并去重 credit，不能留下悬空关系或阻止源 metadata 删除。

### R3. 数据采集与持久化

- TMDb movie/tv 详情请求附带 `credits`，解析 cast 和 crew；movie/tv cast 映射为 `Actor`，crew 中 Director 及 Writer/Screenplay/Story/Teleplay 映射为 `Director`/`Writer`。
- TMDb Episode 详情解析 `guest_stars` 及 crew，映射为 `GuestStar`、`Director`、`Writer`。
- NFO actor name/role、director、writer/credits 进入结构化演职员列表，不再只作为非结构化类型补充。
- provider 成功时优先保存 provider credits；本地 NFO 只补 provider 明确返回空列表的人物类型，其他元数据字段仍遵循现有 provider/no-match 来源优先级。
- 对 provider 成功但某一人物类型为空的情况，只允许 NFO 补该空类型；TMDb 已返回的类型不与 NFO 猜测合并。
- `Match` 必须按人物类型区分“数据源未提供该类型快照”和“数据源明确提供空列表”；只有前者不得改变已有同类型 credit。
- 每次已加载的演员快照替换该目标 metadata 的旧 credit；明确的空快照也应清理已过期 credit，防止陈旧演员残留。
- credit 快照替换自身必须在单事务中完成；失败必须使本次刮削返回错误，重复执行后可幂等收敛。

### R4. Emby 兼容

- Movie、Series 和 Episode item payload 输出按顺序排列的 `People`，至少包含 `Id`、`Name`、`Role`、受支持的 `Type` 和可用的 `PrimaryImageTag`。
- `/Persons`、`/persons` 返回真实人物分页，支持 `StartIndex`、`Limit` 和 `SearchTerm`。
- `/Items?IncludeItemTypes=Person` 与用户路径变体能够返回人物；按 `Ids` 查询时正确过滤。
- `/Items/{personId}` 与 `/Users/{userId}/Items/{personId}` 返回 `Type=Person` 的人物详情。
- `/Items/{personId}/Images/Primary` 通过现有图片代理输出头像；无头像时保持现有透明占位行为。
- 大小写路由维持现有兼容策略，不改变媒体、播放或用户数据接口。

### R5. 可选 AI 中文化

- 复用现有 OpenAI-compatible `AIService`，不增加第三方依赖或新的密钥配置。
- 新增系统设置 `metadata.people_ai_translate`，在管理端以 toggle 暴露，默认关闭；只有 AI provider 可用且该开关开启时运行。
- 批量翻译缺少中文展示值的人物姓名，以及 `Actor`/`GuestStar` 的角色名；导演/编剧类型名不送 AI。
- `Person` 和 `MetadataCredit` 必须同时保留 provider/NFO 原文与中文展示值，重新刮削不能用翻译值覆盖原文。
- AI 输出必须使用结构化 JSON、按输入键校验；空值、缺项、格式错误或请求失败时保留原文并继续刮削。
- 已持久化且原文未变化的翻译不得重复请求；原文变化时清除旧翻译并允许重新生成。
- 人物与 credit 快照必须先完成持久化，AI 翻译不得阻塞或延长刮削主链路。
- 人物翻译输入应携带最多 3 个关联作品标题；角色翻译输入应携带当前作品标题、原始标题、年份和媒体类型。
- AI 请求按最多 100 个唯一条目分批，并同时受输入字符数上限约束。
- 新增持久化翻译缓存：人物名按人物身份隔离，角色名按作品身份隔离；缓存键还必须包含原文、目标语言和提示词版本。
- 服务重启后必须能够重新发现尚未翻译的原文；缓存命中应直接写回，不再调用 AI。
- 异步写回必须校验原文及展示值仍与请求时一致，陈旧结果不得覆盖重新刮削后的数据。

### R6. 回填和范围控制

- 不新增一次性数据库回填脚本；已有媒体通过现有手动媒体库重新刮削显式回填演员。
- 自动扫描只处理正常待刮削数据，不因本功能隐式重抓全部历史媒体。
- 不修改 MediaStationGo 自带 Web 媒体详情页面；前端改动仅限 AI 翻译开关及管理端 API 配置控件。

### R7. 管理端 AI 模型与联网配置

- OpenAI API 配置页面必须可编辑模型，并持久化到现有 `api_configs` 表；数据库值优先于配置文件，空值继续使用配置文件默认值。
- OpenAI API 配置页面必须提供联网搜索开关，默认关闭。
- 开启联网搜索后，面向用户的 AI 对话使用 OpenAI Responses API，并声明 `web_search` 工具；关闭时维持现有 Chat Completions 行为。
- 人物翻译、智能库内搜索和推荐不使用联网搜索；人物翻译使用 Responses API，智能库内搜索和推荐继续走现有 Chat Completions。
- 配置的 Base URL 或模型不支持 Responses API / `web_search` 时返回明确错误，不静默降级为无联网回答。

## Acceptance Criteria

- [ ] TMDb 电影刮削后，电影 metadata 保存按 order 排序的演员、导演、编剧、角色、TMDb person ID 和头像 URL。
- [ ] TMDb 电视剧刮削后，Series 保存演员/导演/编剧；Episode 保存客串及单集导演/编剧，并按类型继承缺失的 Series credit。
- [ ] 包含 actor name/role、director、writer/credits 的本地电影或单集 NFO 能形成结构化人物及 credit。
- [ ] 重复刮削同一作品不会产生重复 Person、PersonIdentifier 或 MetadataCredit；新快照会删除过期 credit。
- [ ] metadata 自动合并后 credit 全部迁移、重复关系合并，源 metadata 可被物理删除。
- [ ] Emby Movie、Series、Episode 详情返回正确 `People`；无演员时返回空或省略字段，不影响其他 payload。
- [ ] `/Persons` 与 lowercase 路由可分页、按姓名搜索；`IncludeItemTypes=Person` 和 person item detail 可用。
- [ ] 有头像的人物返回可访问的 `PrimaryImageTag` 和人物 Primary 图片；无头像返回现有占位图。
- [ ] AI toggle 默认关闭；开启且 AI 可用时批量生成中文人物名和演员/客串角色名，失败或输出非法不影响刮削原文。
- [ ] 原文未变的已翻译 Person/Credit 在重新刮削时保留翻译且不重复调用 AI；原文变化后可重新翻译。
- [ ] credits 入库不等待 AI；后台翻译请求最多包含 100 个唯一条目并携带对应作品上下文。
- [ ] 重启后可重新发现未翻译条目；翻译缓存命中不请求 AI，人物和不同作品角色的缓存上下文不会串用。
- [ ] AI 返回期间原文发生变化时，旧翻译不会覆盖新数据。
- [ ] 现有手动媒体库重新刮削可为已 matched 媒体补齐演员，无需新管理入口。
- [ ] 管理端 OpenAI 配置可保存模型和联网搜索开关，刷新后值保持，运行时优先使用数据库模型。
- [ ] 开启联网搜索后 AI 对话请求 `/responses` 且包含 `tools: [{"type":"web_search"}]`；关闭时仍请求 `/chat/completions`。
- [ ] 人物翻译不受联网开关影响，使用 Responses API 且不携带联网搜索工具。
- [ ] SQLite 聚焦测试覆盖模型约束、replace/merge、TMDb/NFO 解析、Emby People/Persons/detail/image 和回填路径。
- [ ] `go test ./...`、`git diff --check` 通过；若因环境或外部服务无法验证，明确报告未验证项。

## Out of Scope

- 演员作品订阅、追更和主番位过滤。
- 独立人物批量翻译管理页面、别名管理和向外部 Emby 回写 Person 的任务。
- 制片、作曲等其他 crew 类型。
- MediaStationGo 自带 Web 演员展示和人物页面。
- 人物头像导入持久化 ArtworkAsset；本期使用现有安全图片代理与缓存。
- 依据姓名自动合并本地人物与 TMDb 人物。
- 豆瓣演职员兜底及依赖第三方内置应用凭据的 Frodo API 接入。
