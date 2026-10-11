# TMDb 资料中心完整设计（已确认）

用户已确认实施。文中频率、预算与分层边界用于本次实现；尚未部署生产。现状证据见 [research/current-state.md](research/current-state.md)。已确认的展示层级、保留人物身份与来源独立原则直接沿用用户决定。

## 1. 核心边界

```text
TMDb API
   ↓ 统一请求准入、限速、重试、请求合并
TMDb 资料中心：作品 / 季集 / 人物 / 来源关系 / 原始响应
   ├─ 资料中心管理页面
   ├─ 发现、搜索、候选详情：复用来源资料
   ├─ 图片资源：data/catalogs/tmdb/*，缩略图在原图旁
   └─ 刮削 / 本地补全 / 明确的手动刷新
          ↓ 按原有本地应用规则、身份与并发检查
      metadata_items / people / metadata_credits / 本地选图
          ↓
      媒体库、播放、收藏与观看记录
```

- 中心记录不要求存在本地媒体，不为每个搜索结果创建 metadata_items 或 media。
- 中心保存 TMDb 来源事实；本地作品是用户可编辑的展示结果。中心刷新不更新本地标题、简介、人物译名、关系译文、选图或绑定。
- 现有显式本地刷新继续表示“更新本地资料”，并从中心获取所需数据；不会悄悄改成只刷新中心。中心按钮明确命名“刷新来源资料”。
- 复用现有 TMDb API 配置、HTTP传输、图片存储、人物身份、任务中心和主题组件。不新增独立服务、不对其他程序开放、不建设多来源通用资料平台。
- 不全量镜像 TMDb。不因打开作品递归抓所有季，不因遇到演员递归抓其全部作品及其他演员。

## 2. 产品范围与页面

### 2.1 入口与列表

- 保留 `/admin/data-center` 及现有观看空间位置，在页面内增加“豆瓣 / TMDb”来源切换；缺省仍为豆瓣，旧链接不失效。
- TMDb 下为“电影 / 电视剧 / 人物”三个列表；季、集不占顶层列表。
- URL 保存 `provider=tmdb&kind=movie|tv|person&keyword=&quality=&page=`，详情状态用 `subject` 或 `person`，电视剧内部再带季/集选择。非法组合规范化，换筛选回第一页。
- 列表默认每页30条，服务端最多100；仅读数据库，不边列边请求上游。关键词支持标题/姓名、原名和明确的 TMDb ID。
- 筛选采用已有数据可回答的“仅摘要 / 基础资料 / 完整详情、待补全 / 刷新失败”。不要用单一“完整”标签表示图片、履历、所有季集都完成。
- 卡片展示海报或圆形头像、标题/姓名、年份、类型、来源评分/票数（人物不显示评分）、资料级别和最近成功时间。
- 列表“已有本地媒体”仅作为按当前用户权限单独查询的标记。中心有记录不等于有资源；不泄露媒体路径。
- 第一版允许管理员按类型与TMDb ID添加一条资料，或搜索上游后选择补全。中心本地关键词筛选与“搜索TMDb”是两个明确动作，不能在本地无结果时自动联网搜索。

### 2.2 作品详情

- 使用现有 ModalShell、全局浅深主题、圆形演职员组件；点击遮罩/Esc关闭，保留返回位置、焦点和列表滚动，不增加独立主题开关。
- 电影：海报/背景、中文名/原名、年份/上映日、评分与票数、时长、状态、类型、地区、语言、简介、导演/编剧/演员、IMDB等外部链接。
- 电视剧：上述适用字段加首播/最近播出日期、连载状态、季数/集数和季卡片。
- 剧→季：季海报、简介、播出日期、集数、演职员、分页集列表；特殊季 season_number=0 正常保留。
- 季→集：集号、标题、播出日期、时长、评分、简介、剧照，附“本集客串/工作人员”（已取得时）；与季的常规演员分栏。
- 缺季详情显示“尚未补全”，点击该季只补这一季；其 `episodes[]` 一次保存各集基本信息。集扩展信息提供明确的“补充单集详情”，不会逐集自动请求。
- 面包屑“剧名 > 第N季 > 第M集”支持逐层返回，在一个弹窗内切换，不叠多层模态框。
- 常见字段优先结构化展示；管理员可展开“来源记录”查看组件状态和原始JSON。没有值时显示缺失，不推算或用媒体文件时长冒充来源时长。
- 第一版不做预告片播放、剧照图集、海报图库全量同步、评论、推荐关系递归；已有原始响应中的额外字段保留而不强制全部塞进页面。

### 2.3 人物详情

- 人物列表与详情使用现有公共 person ID，增加对应 TMDb profile ID/外部ID。海报墙里的演员点击可跳人物详情；人物作品可返回电影/剧详情。
- 展示来源姓名、别名、原文名、简介、主要职务、生日/逝世日/出生地、头像、外部链接；仅当日期有效时派生年龄，不存会过期的年龄字段。
- 展示“参演 / 幕后”作品，支持电影/电视剧、年份排序及分页。同一作品多个角色合并成一张卡，角色/职务明细不丢失。
- 初次通过作品遇到人物，只建立身份、来源摘要、头像来源及作品关系。人物完整资料与作品履历有各自状态、成功时间和刷新动作。
- 点击人物时补人物详情；首次打开“作品”区域再补履历。取得履历只登记作品摘要与关系，不自动补这些作品的详情、季集或演职员。
- 本地已保存译名可作为辅助显示名，TMDb原文始终独立可见；刷新来源不得覆盖已有 people.Name、人工简介或头像选择。新建人物身份允许从摘要初始化显示名。

### 2.4 权限

- 管理资料中心继续继承现有 adminOnly；导航在观看空间不代表普通用户可访问。管理JSON、任务配置、强制刷新和按ID添加仅管理员。
- 发现仍使用 `can_view_discover`；普通用户使用安全投影，不获得原始JSON、重试错误详情、内部文件路径或管理接口。
- 普通发现用户点击缺失详情，可调用受控的 ensure 动作，只针对已经由中心列表/搜索登记的合法身份补一个组件；不能强制绕过TTL/退避，不能提交远程URL，不能递归创建无限任务。
- 媒体库现有图片鉴权保持原样；新的中心图片接口按所属中心/发现权限校验，不能因为猜出 asset ID 就绕过既有访问范围。

## 3. 数据模型

表名是拟实施命名；下述七张表各有独立职责，均属 TMDb 模块，不引入通用 provider 插件框架。

| 表 | 职责 | 关键身份/约束 |
|---|---|---|
| `tmdb_subjects` | 电影、剧、季、集的来源身份与可查询展示字段 | PK UUID；唯一 `(kind, tmdb_id)`，正ID才入表；parent自关联 |
| `tmdb_person_profiles` | 关联公共人物的 TMDb 来源明细 | tmdb_id 唯一；person_id 唯一FK到 people |
| `tmdb_payloads` | 按组件和语言保存原始响应及真实获取时间 | subject_id/person_profile_id 二选一FK；分别部分唯一 `(owner, component, language)` |
| `tmdb_credits` | TMDb 来源演职员及人物履历关系 | subject_id、person_profile_id、origin、credit_key；不替代 metadata_credits |
| `tmdb_artworks` | 作品来源图片引用与下载状态 | 唯一 `(subject_id, artwork_type)`；引用 artwork_assets，可空等待下载 |
| `tmdb_query_cache` | 搜索/榜单顺序、分页和短期查询缓存 | 规范化查询键唯一，不承担完整资料真源 |
| `tmdb_refresh_jobs` | 资料补全/刷新的去重、租约和失败恢复 | owner二选一；唯一 `(owner, scope, language)`；每个作用域一个持久任务槽 |

### 3.1 作品身份与层级

- `kind=movie|series|season|episode`，对外接口 `tv` 映射内部 `series`，禁止混用导致同数字电影/剧冲突。
- `parent_id`：季必须指向 series，集必须指向 season；movie/series无parent。
- 季唯一 `(parent_id,season_number)`，season_number>=0；集唯一 `(parent_id,episode_number)`，episode_number>0。数据库约束非空及类型组合，repository事务校验父类型。
- ID 和坐标同时冲突时记录冲突，不自动把旧记录换绑到另一作品；禁止将本地扫描季号作为来源事实。
- 只有带合法正TMDb ID的摘要才建立来源实体；已有明确父ID而缺父记录时可建 identity-only 父记录。缺父ID、季集坐标无法验证的历史记录保留旧快照待修复，不猜父关系。
- 可查询字段：标题/原名、简介、发行/播出日期、年份、状态、语言、地区、类型、时长、评分、票数、外部ID、季集数量。列表使用这些列或小JSON，不扫描大型payload。
- `quality` 表示来源资料级别：identity/summary/base/detail；集另标 `derived_from_season`，它不等同单集扩展详情。图片与人物履历完成度单独返回。
- 本地作品不新增强制FK；继续通过 `metadata_identifiers(provider=tmdb, kind, external_id)` 关联中心。改绑只改变本地指向，旧来源记录继续保留。
- 评分更新在同一事务提交来源投影和对应payload；只更新评分列而留旧基础来源不是本方案。上游基础请求本来就包含基础字段，不额外构造评分专用抓取器。

### 3.2 公共人物与来源人物

- people/person_identifiers/metadata_credits 保留现有ID和用途，已有关系不搬表。
- 导入人物按 `person_identifiers(provider=tmdb,external_id)` 查人，无则创建公共人物及标识；事务+唯一约束处理并发，不按同名自动合并。
- tmdb_person_profiles保存source_name/original_name、别名、简介、生命日期、出生地、known_for_department、profile_path等源字段；保留全部未投影字段到payload。
- 来源头像保存自己的 `profile_image_key` 和对应源路径，不直接改公共 people 的人工头像；新人物/明确的本地人物补全可复用同一个文件键，不复制图片字节。
- profile/person_identifier 的TMDb ID必须一致；冲突不可通过“再建一个同名人物”绕过。复用原 repository 的软删恢复检查，FK使用RESTRICT；不新增人物自动合并功能。
- 删除本地媒体或作品不会删除profile。未来若做人物合并，必须事务迁移 profile、identifiers 和两类credits；本轮不实施重编号、重建人物表或重定向框架。

### 3.3 原始组件与新鲜度

- `tmdb_payloads` 保存原始JSON，component限定为 summary/base/detail/credits/filmography/season_episode；已存在详情的附加对象可以由detail投影，不强制重复存每一个JSON子字段。
- 原始响应与展示投影分开。中文主请求与原始语言补充按language分行，保留真实响应；投影可组合，不能将合成JSON宣称上游原始响应。
- 每行记录 fetched_at、payload_origin（api/search/season/legacy）、quality、content_hash。每个scope的 last_success_at/next_refresh_at、失败和运行租约在job槽中，不把组件调度时间塞进原始响应。失败不修改成功时间。
- 一次detail响应可以满足base、detail、credits等多个scope，事务更新各已加载scope的成功时间、next_refresh_at及payload引用，不额外HTTP请求，也不必把同一原始正文重复存多行；基础刷新只推进base槽。
- 已加载范围必须同时由请求计划和有效响应结构证明。未请求append、字段缺失、null或结构不合法均不能推进对应scope；detail成功不代表全部扩展成功。credits中的cast/crew分别验证并替换，合法空数组才可清空对应范围；只有两者均有效才标整个credits成功。external_ids等扩展同样按实际有效组件推进，保留其它旧值及时间。
- 仅保留每owner/component/language最新有效版本，不做无限历史归档；同组件较旧请求不得覆盖新成功结果。按请求开始时观察版本或generation做CAS，成功后事务更新投影及已加载范围。
- 摘要只能初始化或补空；不得把搜索摘要刷成detail、延长detail TTL，或覆盖更高质量详情。
- 基础刷新写base，不覆盖旧detail中的credits/external_ids等扩展。集的season_episode基本字段可较新，但不能抹掉旧detail扩展；展示按字段组选择最新权威来源。
- 旧导入且语言无法证实的payload标 `language=und,payload_origin=legacy`，不编造API获取时间；旧fetched_at有效则继承，无则标未知/待补全，不使用迁移执行时间冒充刚抓取。
- 缺失对象表示“未加载”，有效空数组表示“已加载且无数据”，null评分/日期不变成0。关系替换只作用于已确认加载成功的范围。

### 3.4 来源关系与履历

- tmdb_credits必须保存演员、导演、编剧之外的上游部门/职务，不受本地四类credit枚举限制。
- 字段含 subject_id、person_profile_id、origin、credit_key、credit_id、cast/crew类别、character、department、job、order，必要的角色次数/客串集上下文保存小JSON。
- `origin=subject` 的作品演职员与 `origin=person` 的人物履历分别同步。人物履历不能清空作品演员表，作品演员变化也不能宣称人物履历已全量刷新。
- credit_key为稳定JSON数组 `[cast或crew, credit_id或空, character, department, job, episode_tmdb_id或空, role_id或空]` 的哈希；缺失值统一为空，字符串仅去首尾空白，保留大小写和内部字符。键不含order、popularity或本地译文等可变展示字段。唯一 `(subject_id,person_profile_id,origin,credit_key)`；同credit_id的不同角色、不同credit_id的相同角色均保留。
- 无独立credit_id且上述语义完全相同的项可去重，原始payload仍保留原文；不按响应位置造键或凭重复次数推算参演次数。主语言响应决定关系集合，原语言响应只补约定的空来源文字字段，不另建一套重复关系或覆盖中文主集合。
- 刷新事务按owner及origin/已加载范围替换，保留另一来源范围。展示重复证据合并，工作详情优先subject来源；人物页区分“完整履历”与“本地已知关联”。
- 人物履历以一次完整combined-credits响应为拟接入契约，UI分页只查数据库，不按翻页重复请求上游。验证完整响应后，事务替换该人物的 `origin=person` 关系及作品摘要；超限、响应截断、请求失败或范围缺失均保留旧集合，不用搜索known_for充当全履历。实施时先核对官方样本；若实际端点存在分页，必须收齐并验证全部页后才可替换，首屏响应不能标全履历成功。
- 季响应中的每集guest_stars/crew若存在可存episode来源关系；未返回时标未加载，不能把季常规演员标成每集明确参演。
- 本地 metadata_credits 保持季级继承规则，中心不写本地集级关系。显式本地资料应用仅转换当前本地支持的类型，沿用已有译名/角色保留策略。

### 3.5 图片与查询缓存

- tmdb_artworks管理来源当前poster/backdrop/still，不改metadata_artworks的本地选图。第一版不为全部图片图库建表/下载。
- 字段保存 TMDb image path、已成功下载对应路径、asset_id、pending/ready/error/no_image、attempts和next_retry_at。上游明确无图与下载失败分开。
- 人物头像状态同理放profile上，不为一张头像再建公共 artwork 资产；复用现有PeopleImageStore存储格式。
- tmdb_query_cache保存 operation、规范化参数摘要、language/region/安全筛选、page、ordered IDs和排名/分页信息、fetched_at/expires_at。正文不保存私有媒体状态。
- 推荐搜索TTL1小时、发现榜单TTL6小时；用户刷新榜单可强制更新该查询页，但不连带强刷每个作品详情。
- 查询参数键包含base/config revision以避免切换API镜像时复用错误结果；密钥原文不入键、日志或表。持久作品身份不随密钥轮换改变。
- 搜索结果中 summary+关联图路径只落库，不同步逐条请求详情或下载图片；本地查询列表不写新资料。

### 3.6 索引

- subjects：上述身份/坐标唯一约束；`(kind,updated_at,id)`分页、父项列表索引、评分/年份按实际排序需要增加组合索引。
- profiles：tmdb_id、person_id唯一；显示名/原文名搜索沿用项目关键词查询方式，先测实际计划，不默认新装扩展。
- payloads/jobs：owner+component或scope+language部分唯一；job到期/待执行索引 `(next_refresh_at,id)`、`(status,next_attempt_at,priority,id)`；running lease_until部分索引。
- credits：作品和人物两个方向索引，替换范围包含origin；artworks的asset_id反查索引用于保护文件引用。
- query cache：唯一query key、expires_at清理索引。列表计数与分页共用条件，采用稳定排序；后台扫描使用keyset，禁止逐卡查库/N+1请求。

## 4. 统一获取与请求成本

新增明确的 TmdbCatalogService/Repository（名称实施时遵循项目风格），TMDbProvider只负责传输与解析。所有来源请求统一经过中心的Get/Ensure/Refresh策略；禁止业务绕开中心拿到详情后只写旧快照。

| 操作 | 读取/写入边界 | 不展开的范围 |
|---|---|---|
| 搜索/榜单 | 一页查询缓存，摘要批量upsert，返回中心图片URL | 不抓每条详情、季集或人物履历 |
| 打开已有作品 | GET读库；缺详情时单独ensure一个作品 | 不抓每季/每集 |
| 查看季 | 已有则读库；缺详情ensure这一季，批量拆episode summaries | 不逐集详情请求 |
| 查看集 | 读季派生基本信息；明确补充时请求单集扩展 | 不刷新全季/整剧其它集 |
| 点击演员 | ensure该人物详情 | 不自动抓整份履历或其作品详情 |
| 打开人物作品区域 | ensure一个人物的履历，登记关联作品摘要 | 不递归补作品、演员或图片 |
| 本地刮削/维修 | 根据既有任务意图获取指定中心组件，再应用本地规则 | 不改变原任务目标及媒体范围 |

- 现有movie/tv详情含credits等append组件，可一次请求返回详情并落各已加载范围；并不为了多张表多发请求。
- 全量详情、基础刷新和人物履历是不同scope。基础请求只取基础端点；不为评分变化重取演职员、履历、每季每集或图片文件。
- 每次季获取优先中文；需要时原始语言补文字，沿用最多两份季响应（另计未知剧语言查询）的规则。不随意再请求第三语言。
- 人物详情、人物履历、人物搜索拟分别接person details、combined credits、person search能力；当前项目缺少这些实现。实施第一步用官方schema/响应样本验证语言/append和分页字段，不依赖网页抓取，不承诺外部文档未验证的字段必有。
- 本地冷GET只返回已知数据和 `missing_scopes`；UI在用户打开该层后POST ensure，后台结果完成再读回。资料GET不暗中更新canonical数据。
- 取消页面请求停止等待和后续UI写入；已被其他消费者共享的任务不被单个页面取消。后台任务持有服务生命周期context，关闭时取消并归还租约。

## 5. 图片统一与剩余cache

- 作品图片：`data/catalogs/tmdb/artwork/aa/bb/cc/<hash>.<ext>`；人物：`data/catalogs/tmdb/people/aa/bb/cc/<hash>.<ext>`。
- 缩略图：同层 `<hash>.thumbnails/v1/<spec>`；沿用刚完成的迁移，不再换目录。
- 中心给前端返回稳定内部图片URL（subject+类型或person），不返回远程URL让 imageURL 转 `/api/img`。
- 图片入口优先读已有asset/key；没有才按DB记录的TMDb image path下载。既有同来源映射先复用；下载后按内容hash存一份。不能仅凭相同标题或未经验证的URL推测同图。
- 中心导入使用直接下载/共享进行中请求；可以读取已有 cache/images 作为过渡命中来源，但不为新中心下载再写一个中间cache副本。图片 URL变化后新旧引用原子切换；失败保留旧图并使用合适的no-store策略，不把旧图记成新URL下载成功。
- 首次图片GET可触发受控下载，成功存储后直接服务；列表响应不等待几十张图片下载。图片失败有持久退避，同图同请求合并；强制图片重试只作用于该图。图片状态由artworks/profile持有，不为每个图片再建来源JSON刷新job。
- 初次展示下载海报、背景和头像按实际请求发生；周期来源刷新只登记源路径变更和pending状态，不同步下载全中心图片。无本地作品关联的中心图片在首次展示或管理员单图重试时下载，不因登记摘要就启动后台全量下载。
- 修复任务只处理当前被引用的缺失/失败/变更图片，不扫描并重新下载全部旧文件。共享asset继续保留引用保护，不主动删除。
- 发现、搜索、手动候选、详情及人物页的TMDb图片均需改成中心URL，不能只改资料中心页面。
- `cache/images` 和 remote/local variants 仍可服务非TMDb直链或用户旁挂图片；本设计不宣称整个cache目录退休，也不迁移其他provider的业务取图链路。

## 6. 刷新策略（推荐默认值）

### 6.1 分组件TTL

“完整”表示取得过哪些范围，不表示永久免刷新。成功时间按组件推进，不因浏览、图片下载、翻译或关系写入延长。

| 数据范围 | 推荐到期规则 |
|---|---|
| 搜索摘要/发现页 | 查询TTL分别1小时/6小时；只更新命中摘要，不覆盖已完成详情 |
| 即将上映、上映后90天内电影 | 基础资料24小时 |
| 已上映超过90天电影 | 基础资料30天 |
| 连载、计划中电视剧 | 剧基础资料24小时 |
| 完结/取消电视剧 | 剧基础资料30天 |
| 正在播出或最近30天有更新的季 | 已纳入维护的季清单24小时，一次取季拆集 |
| 较早季/特别篇 | 30天；未知播出状态保守7天，不视为完结 |
| 作品/季演职员及扩展详情 | 活跃作品7天；稳定作品90天 |
| 人物基本资料 | 30天 |
| 人物作品履历 | 曾主动打开/维护过的履历30天；未加载者不自动补 |
| 独立单集扩展 | 默认按需/手动，不全库定时逐集请求 |
| 图片字节 | 同源path未变且本地存在则不下载；缺失/失败/源变化按修复状态处理 |

- 尚未取得详情的海量搜索摘要不因TTL到期就自动升级。
- 周期候选限：已有成功基础/完整资料且到期，或已明确排队的补全/重试。仅身份/摘要、未打开人物履历不自动加入。
- 明确的本地缺项复查可请求某scope提前更新，但与中心分享成功时间、未找到时间及任务锁，避免两个定时器重复取同一季。
- 空字段本身不是立即无限重试条件；上游确认无值记录成功及正常TTL。请求失败保留上一次有效内容。

### 6.2 通用任务与预算

- 新任务 definition/kind：`tmdb_data_refresh`，名称“TMDb资料刷新”，放任务中心“通用”。默认关闭，建议启用后每小时运行一轮。
- 每轮默认最多100个**实际TMDb HTTP请求**，配置范围1–1000；含重试、语言补充和独立credits请求。不是100部剧，不把图片下载混入这个数。
- 按优先级和到期时间选择已登记组件；不提前载入全表。预算用完时未完成组件保持待办，下轮继续；成功组件不重跑。
- 新任务首次启用不自动补齐所有摘要/所有人物；界面显示到期、待办和本轮预算，避免把任务耗尽预算误报全中心完成。
- task-center手动运行该任务仍执行“到期+限额”；单条“立即刷新来源”才绕过成功TTL，但不能绕过请求限速、上游429冷却或取消。
- 默认来源HTTP并发2、请求起始间隔至少250ms，作为本项目的保守调度值，不宣称TMDb官方限额。图片下载使用独立的现有受控通道，不占来源JSON并发。
- 新任务与现有刮削/维修的TMDb请求共用准入、正在执行的同scope请求与落库结果；不形成互相独立的重试风暴。
- getJSONAttempt层计数每次HTTP尝试并消耗预算；不能只数业务方法。429应尊重完整Retry-After：短等待可等待，长等待持久保存冷却并结束本轮，不在30秒截断后继续打上游。
- 同一API配置下429暂停整个来源批次；401/403停止本轮并给配置错误，不把每条资料都刷成失败；404保留旧记录，重试默认72小时；临时错误5分钟、30分钟、2小时、24小时递增退避。
- `tmdb_refresh_jobs`用原子claim、5分钟lease及必要续租；完成写回校验claim token/generation。过期worker不能覆盖新结果；重启回收过期running，刷新不持有媒体刮削全局写锁。
- 配置级冷却必须持久化并跨轮生效，新任务和重启后的worker同样检查；延期任务释放lease，不占执行槽等待长冷却。每轮独立分配预算，未完成任务下轮重新准入，重启不能清掉上游冷却。
- 一次抓季存在语言补充时每个已验证响应可独立落原始payload，但完成中文投影/已加载范围必须标部分完成；预算耗尽不伪报整季已补全，也不丢已完成请求供下轮复用。
- task metrics：selected_entities、selected_scopes、cache_hits、http_attempts、updated、unchanged、failed、deferred、remaining_due、images_pending；显示真实请求数与实体数。日志脱敏，不记录密钥、完整请求URL或原始payload。

### 6.3 现有任务的分工

| 现有任务 | 保留职责 | 接入中心后变化 |
|---|---|---|
| `tmdb_artwork_local_repair` | 本地资料所需图片下载/修复 | 复用中心asset及图片状态；仍以现有本地资料缺图为任务范围，不扩成全中心海量图片下载 |
| `tmdb_artwork_missing_recheck` | 本地作品缺图的上游复查 | 经中心更新对应基础图路径，保留原目标筛选与冷却 |
| `tmdb_episode_metadata_recheck` | 有媒体的季集缺项修复 | 通过中心取季，按原规则应用本地；不把全中心无媒体季集加入 |
| `tmdb_snapshot_backfill` | 历史“本地绑定有ID但缺来源”补缺入口 | 迁移期保留key/历史；切换后指向缺中心组件，不再写旧快照 |
| 新 `tmdb_data_refresh` | 与媒体无关的来源资料到期刷新 | 只更新中心，不执行本地字段补齐 |
| 新 `tmdb_center_migration` | 只读旧数据、导入新中心的有限迁移 | 无上游请求；启动异步一轮/失败手动重试，有独立进度 |

保留现有稳定definition key、schedule设置、历史与日志归属。不要因为底层原先共用TaskKindArtwork就混淆任务定义。

## 7. API与交互契约

以下是拟新增接口；existing API保留，内部逐步接中心。

| 接口 | 作用 |
|---|---|
| `GET /api/admin/tmdb/subjects?kind=movie|tv&...` | 本地分页列表、轻量状态 |
| `GET /api/admin/tmdb/subjects/:id` | 作品/季/集详情，按需带原始组件 |
| `GET /api/admin/tmdb/subjects/:id/seasons` | 剧下季列表，仅当前父项 |
| `GET /api/admin/tmdb/subjects/:id/episodes?page=...` | 季下集列表，分页 |
| `GET /api/admin/tmdb/people`、`/:personID`、`/:personID/works` | 人物列表、详情、分页履历 |
| `POST /api/admin/tmdb/subjects/:id/refresh` | scope=base/detail/season/episode_extended，受类型限制 |
| `POST /api/admin/tmdb/people/:personID/refresh` | scope=detail/filmography |
| `POST /api/admin/tmdb/subjects/:id/images/:type/refresh`、`/people/:personID/image/refresh` | 管理员单图重试，复用下载实现 |
| `POST /api/admin/tmdb/import` | kind+正TMDb ID，仅登记并排队指定对象 |
| `POST /api/admin/tmdb/search` | 类型、关键词、页，读写查询缓存和摘要 |
| `POST /api/catalogs/tmdb/subjects/:id/ensure` | 受发现权限保护、补当前层所需缺失/到期范围 |
| `POST /api/catalogs/tmdb/people/:personID/ensure` | 受发现权限保护、补人物detail或用户展开的filmography |
| `GET /api/catalogs/tmdb/subjects/:id/images/:type` | 中心来源图片，不接受任意上游URL |
| `GET /api/catalogs/tmdb/people/:personID/image` | 来源人物头像，公共people图片API仍保留 |

- 管理subject的:id是中心UUID，人物:personID是现有公共person UUID；按TMDb数字查找/导入有单独参数，避免混淆。
- 新公共ensure只针对已登记中心身份，白名单scope，每请求最多一个对象/组件；服务端限制每用户未完成ensure数量（建议50）并合并同对象请求。重复返回同job，不重复排队。
- 冷ensure返回202+job状态；命中新鲜资料返回200+中心投影。强制刷新返回202，通过原详情状态轮询，完成后读实际落库结果，不用返回的网络临时值覆盖UI。
- 返回component_states（loaded/fresh/stale/pending/error/not_found）、各成功时间、next_retry_at、quality及missing_scopes。普通用户错误是安全摘要，管理员才能查看受控错误详情。
- 列表/详情 Cache-Control 为private/no-store；图片沿用现有鉴权和按内容稳定版本规则。所有列表请求可取消，旧响应不得写入已切换的对象或账号。
- 400非法类型/坐标/ID；401/403权限；404不存在本地身份；409身份冲突；429应用准入或来源冷却附retry_after；503启动未就绪/配置不可用；上游失败任务状态可见且保留旧资料。
- 不支持全库强刷按钮、批量任意URL抓图或跨全部季集的一键递归操作。明确需要整剧深度补齐时，继续使用有独立意图和指标的既有目录补全任务。

## 8. 接入与兼容矩阵

必须完成以下入口盘点及测试，才称“TMDb统一走资料中心”：

1. TMDb发现榜单、搜索结果和手动刮削候选：摘要进中心，图片改内部URL；query cache保序，不改变过滤后has_next语义。
2. 发现详情：读取中心来源投影；已存在本地作品可继续展示本地译名/选择的展示层，但来源请求只经过中心。移除管理员打开弹窗自动刷新本地metadata的副作用，提供显式本地刷新入口。
3. 普通自动刮削、手动匹配、目录hydrate：改为中心Ensure详情后应用本地，已有详细响应不得丢弃后再请求一次；查询摘要不会自动调用canonical hydrate。
4. TMDb ID编辑：中心先取得并验证新身份；本地identifier改变和本地接受记录在事务里提交。失败保留旧绑定；新中心记录可以独立保留，不因本地事务失败删除。
5. 本地作品、季集手动刷新和现有维修任务：读取指定组件，保留现有媒体选择、scope及并发CAS；失败不把旧作品字段清空。
6. 人物补全/头像/翻译：来源detail与履历先落profile；翻译和人工字段继续属于people/本地credits，不与来源成功时间共用。
7. `UpsertProviderSnapshot`、`FindProviderSnapshot`、缺快照枚举和快照回填：TMDb分支过渡到中心，所有直接查旧TMDb快照SQL必须替换，不能只改service helper。
8. 作品合并/删除、identifier改绑：只改变本地关联，独立来源不被级联删除；原人物ID和媒体/播放/收藏身份不变。
9. Web/Emby provider状态与评分：本地作品评分保持现有选用语义；中心来源评分另有明确字段，不能突然把所有媒体显示分替成TMDb最新分。需要的来源详情经批量中心join，零逐项联网。
10. 图片repair、API、发现预热：复用资料图片引用，中心已入库图不因外部URL投影再通过cache/images复制。

这会改变部分既有发现契约（远程详情不留资料、管理员打开即刷新本地）；必须同批更新spec、端点测试和浏览器检查。不会改变媒体可见性、是否可播放、库资源统计或公共person ID。

## 9. 历史迁移、切换与回滚

### 9.1 先加表、后迁移、不联网

- 建新表/索引不删旧表，先接统一来源写入及读取适配，再启动有界后台迁移。不会在启动监听前同步搬几十万资料。
- 原图片路径已经统一，本轮只建立来源引用；优先复用现有tmdb来源asset/key，不能重下载或再复制全部原图。
- 按旧 metadata identifiers/snapshots keyset分页：movie/series→season→episode；每批事务，逐行幂等。未关联媒体的元数据同样迁移。
- 使用合法ID、kind、父链、JSON根ID和源组件质量校验。冲突、损坏JSON、缺父链记录失败并保留原记录，不能静默跳过后宣称完整迁移。
- 完整movie/tv/season/episode响应按detail导入；季派生episode按season_episode导入；同一集两者并存。不拿手工改过的metadata字段伪造TMDb原始详情，只能生成标注为legacy_projection的identity/摘要。
- 旧人物只有credits摘要时仅建立profile摘要，不标为人物详情完整；沿用person identifier复用person ID。旧本地角色译文仍在metadata_credits，不冒充源原文。
- 旧选图仅在来源明确tmdb且匹配对应source path时挂到中心。人工图片、其他provider选图保持本地；中心另有来源path时标待下载而非猜关联。
- 保留原成功时间，迁移不把所有记录瞬间变新鲜，也不立即为全部待补摘要排远程任务。
- 每条成功来源行/组件就是检查点；重复导入只补空/升级质量，不能覆盖迁移期间新抓的高质量或较新组件。identity CAS保护并发改绑。

### 9.2 切换顺序

1. Schema与中心repository、原始组件质量规则完成；使用隔离数据验证。
2. 引入统一获取及旧接口适配，所有新TMDb数据只写中心。迁移未覆盖部分可由适配层读旧快照并入迁移待办，不能建立永久双写。
3. 后台迁移，任务中心报告候选/成功/冲突/失败/剩余；新中心与旧关联按数量和样本/全量身份一致性校验。
4. 读入口全部切换；旧 `FindProviderSnapshot` 如需兼容，返回中心派生视图并以所需组件最早成功时间表示保守新鲜度，不能拿最新base时间宣称所有扩展刚刷新。
5. 普通业务不再写或直读 `metadata_provider_snapshots` 的TMDb行；保留旧行一段观察期供人工核对。
6. 清理属于独立后续操作：只有身份/组件迁移无缺口、引用完整、读写扫描和重启验证通过才列出清理SQL。只删旧表中provider=tmdb的已验证行，不能删除其它provider、整个表或本地metadata字段。

### 9.3 完成条件与风险

- 完成标记以无未处理/未解释失败为前提；实际失败需要修复或明确接受后才能结束迁移，不用单一“循环跑完”代替成功。
- 新请求失败不丢旧资料，历史数据始终保留；中断恢复仅处理仍缺组件/未成功批次。
- 推荐先部署带中心兼容适配的过渡版本，再切全量读。可以回退到该过渡版本；不承诺直接回滚到完全不认识中心表的旧程序仍能读到新增资料。
- 第一版不做旧快照删除、无关联人物清理、旧图片自动清理、存储回收或资料中心批量删除；这些不应混进来源迁移。

## 10. 验证与验收

| 场景 | 必须证明 |
|---|---|
| 同号movie/tv/person | 身份互不混淆；季0合法；孤立/错父链不能错绑 |
| 一次季详情 | 多集基础资料入中心，不产生逐集HTTP请求，不覆盖扩展快照 |
| 作品多演员/多角色 | 保留完整来源职务与角色，人物只有一份身份，不触发履历递归 |
| 人物履历 | 完整响应才事务替换origin=person；失败不截断旧集合，页面翻页只查库；只新增作品摘要、不递归抓资料 |
| 角色键与组件覆盖 | 多角色不合并丢失、响应排序变化不造重复；缺失/null/未请求append不算成功，有效空只清已验证范围 |
| 摘要/基础/完整响应竞争 | 较旧/低质量不能降级，base刷新不清credits，译名与本地字段不变 |
| 手工修订与改绑 | 中心刷新前后本地字段/ID/选图不变；慢请求不会覆盖新绑定 |
| 并发/限额/重启 | 同scope只抓一次，取消单个等待者不误取消共享任务；100次预算含重试，不超额；旧lease不能提交 |
| 429/401/404/空数组 | 全局冷却、停止配置错误批次、保留旧值、仅明确加载空范围才替换 |
| 图片 | 中心图内部URL、只存正式原图、旁置缩略图、相同来源复用、修复只处理缺口 |
| 老数据迁移 | 无媒体数据也保留，保留时间、完整episode、人物ID、选图；可重跑、中断及异常有清单 |
| 权限与页面 | 管理员中心、普通发现安全投影、URL直达权限一致；季集/人物返回状态和主题正常 |
| 既有业务 | 刮削/ID编辑/媒体刷新/season recheck/Emby图片与人物身份不退化；ownership标记仍按可见媒体 |

测试使用真实隔离PostgreSQL（跳过不算通过）、mock TMDb响应及HTTP请求计数、race、静态检查；Web执行lint/build及页面自动化。只读查生产数据不等同迁移成功，实际部署再做一次原始字段和引用核验。

## 11. 实施阶段与明确不做项

- 阶段A：来源schema、原始组件/质量规则、人物身份复用、只读历史迁移，先不改所有页面。
- 阶段B：统一API获取/查询缓存/请求准入/任务状态，接入所有TMDb详情写入口与兼容读取。
- 阶段C：图片入口统一、独立刷新及旧维护任务协作，验证请求预算和重复下载消除。
- 阶段D：资料中心电影/剧/人物页面、季集层级与人物履历；更新发现消费者及权限/组件状态。
- 阶段E：全链路审查、迁移完整性核验及实际部署；旧数据清理另行执行。

上述阶段的代码和验证进展见 implement.md，实际部署尚未执行。不包含TMDb全站同步、跨来源自动合人、多语言任意切换、图集/预告片、外部开放API、删除无关联来源、自动覆盖用户本地编辑。官方人物API文档和模拟数据浏览器验证已完成，真实上游与生产迁移仍需部署后核验。

## 实施契约补充

- 首次抓取前可能只有 API 坐标，refresh job 以无凭据规范化 path/query/config hash
  唯一；成功后关联 source owner。一个来源组件的不同请求计划可能对应不同槽。
- 定时与显式待办通过 explicit_pending 区分，旧配置槽交接后标 retired。
- 实际图片路由为 `/api/catalogs/tmdb/images/:kind/:tmdbID/:type`；身份必须已经登记。
- 官方人物详情、搜索、combined credits 文档已于 2026-10-11 使用浏览器核对；
  combined credits 无分页参数，返回 cast/crew/id，搜索独立分页。生产 API 未联调。
