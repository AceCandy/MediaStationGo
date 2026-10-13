import type { EmbyApiEndpoint, EmbyApiParameter, EmbyApiResponse } from './embyApiCatalog'

export const EMBY_EXT_API_CATEGORIES = ['扩展能力', '榜单', '日历', '媒体匹配', '扩展图片'] as const
const token: EmbyApiParameter = {
  name: 'X-Emby-Token', location: 'header', type: 'string',
  description: '使用 Emby 登录令牌；也支持 Bearer、MediaBrowser 和 api_key。所有扩展接口均校验账号有效性及 can_view_discover 权限。',
}
const errors: readonly EmbyApiResponse[] = [
  { status: '401 / 403', contentType: 'application/json', description: '未登录、令牌失效、账号不可用或没有访问权限。' },
]

export const EMBY_EXT_API_ENDPOINTS: readonly EmbyApiEndpoint[] = [
  {
    id: 'ext-capabilities', category: '扩展能力', name: '扩展能力查询', methods: ['GET'],
    path: '/emby-ext/v1/capabilities', auth: 'token', support: 'implemented',
    description: '查询扩展版本、榜单、公共周历与媒体匹配的支持范围。', parameters: [token],
    responses: [
      { status: '200', contentType: 'application/json', description: '能力说明；calendar.enabled 表示日历来源当前是否启用。',
        example: '{"version":"1","rankings":true,"calendar":{"enabled":true,"source":"bangumi","mode":"weekly","timezone":"Asia/Tokyo","exact_airtime":false},"library_matching":{"sources":["tmdb","douban","bangumi"],"max_items":100,"identity":"external_id_and_media_type","scope":"metadata"}}' },
      ...errors,
    ],
  },
  {
    id: 'ext-rankings', category: '榜单', name: '榜单目录', methods: ['GET'],
    path: '/emby-ext/v1/rankings', auth: 'token', support: 'implemented',
    description: '返回资料中心已支持的 TMDb、豆瓣榜单及来源可用状态。', parameters: [token],
    responses: [
      { status: '200', contentType: 'application/json', description: 'items 数组：id、name、source、enabled、page_size。TMDb 每页 20 项，豆瓣每页 24 项；末页可更少。' }, ...errors,
    ],
    notes: ['不要求客户端内置榜单清单；先读取目录，再用 id 请求内容。'],
  },
  {
    id: 'ext-ranking-items', category: '榜单', name: '榜单内容', methods: ['GET'],
    path: '/emby-ext/v1/rankings/:id/items', auth: 'token', support: 'implemented',
    description: '复用来源资料中心缓存，保留来源顺序与分页，不混入其他来源的替代榜单。',
    parameters: [token,
      { name: 'id', location: 'path', type: 'string', required: true, description: '榜单目录返回的 id，如 tmdb_trending_day。' },
      { name: 'page', location: 'query', type: 'integer', description: '1–500，默认 1；按来源原生分页，刷新后跨页顺序可能变化。' },
    ],
    responses: [
      { status: '200', contentType: 'application/json', description: 'id、source、page、page_size、items；total 与 has_more 为 null（来源适配层未提供准确总量），可请求下一页直至空页或页码上限。', fields: [
        { name: 'items[]', type: 'object', description: 'source、media_type、title，以及按来源提供的 tmdb_id / douban_id；可选 overview、original_name、year、rating、poster_url、backdrop_url。' },
      ] },
      { status: '400 / 404 / 502 / 503', contentType: 'application/json', description: '页码无效 / 榜单不存在 / 来源请求失败 / 来源禁用或未配置，返回 error。' }, ...errors,
    ],
    notes: ['相对图片路径以 MSG 服务地址解析并携带同一令牌；第三方绝对图片 URL 不得附带 MSG 令牌。', '榜单沿用资料中心已有缓存刷新策略，不承诺所有来源均使用周历的异步刷新策略。'],
  },
  {
    id: 'ext-calendar', category: '日历', name: '公共放送日历', methods: ['GET'],
    path: '/emby-ext/v1/calendar', auth: 'token', support: 'implemented',
    description: '返回完整 Bangumi 公共周历；不截取为发现页的 24 项推荐行。',
    parameters: [token, { name: 'weekday', location: 'query', type: 'integer', description: '可选 1–7，周一至周日；省略时返回完整周历。' }],
    responses: [
      { status: '200', contentType: 'application/json', description: 'source、mode=weekly、timezone=Asia/Tokyo、items、updated_at（最近成功快照的采集开始时间，UTC）、stale。', fields: [
        { name: 'items[]', type: 'object', description: '作品字段含 source=bangumi、media_type=anime、bangumi_id、title、可选海报/简介/年份/评分；另有 weekday、timezone。broadcast_time、season、episode 未知时为 null。' },
      ] },
      { status: '400 / 502 / 503', contentType: 'application/json', description: '星期无效 / 冷启动无可用日历 / 来源禁用，返回 error。' }, ...errors,
    ],
    notes: ['持久快照 6 小时内直接读取；过期访问先返回旧数据并触发后台刷新。首次无缓存需等待；刷新失败保留旧数据并退避 1 分钟。', '每周放送规则，不代表精确播出时间、历史日期范围或个人追剧日历；不做个人账号授权同步。', '播放仍通过 Emby 实时获取；日历缓存不缓存播放地址。'],
  },
  {
    id: 'ext-matches', category: '媒体匹配', name: '媒体库精确匹配', methods: ['POST'],
    path: '/emby-ext/v1/library/matches', auth: 'token', support: 'implemented',
    description: '按来源 ID 和作品类型匹配当前用户可见且有文件的普通元数据作品，返回 Emby 媒体项 ID。',
    parameters: [token,
      { name: 'items', location: 'body', type: 'array', required: true, description: '1–100 项；每项 key（1–128 字节，批次内唯一）、source（tmdb/douban/bangumi）、external_id（正整数字符串）、media_type（movie/tv/series/anime）。整个 JSON 最大 64 KiB。' },
      { name: 'items[].season / episode', location: 'body', type: 'integer', description: '可选，必须成对提供且为剧集；season 为 0–10000，episode 为 1–100000，用于精确分集匹配。' },
      { name: 'X-Play-Profile-ID', location: 'header', type: 'string', description: '可选当前用户播放配置；也支持 profile_id 查询参数。省略使用默认配置。' },
      { name: 'X-Play-Profile-PIN-Token', location: 'header', type: 'string', description: '受 PIN 保护的配置需有效解锁令牌，也支持 profile_pin_token 查询参数；未解锁时匹配为空。' },
    ],
    requestExample: '{"items":[{"key":"movie-1","source":"tmdb","external_id":"101","media_type":"movie"}]}',
    responses: [
      { status: '200', contentType: 'application/json', description: '按请求顺序返回 items，每项 key 和 matches 数组；匹配对象为 key、item_id、item_type（Movie/Series/Episode）。隐藏、无文件或未匹配统一返回空数组。', example: '{"items":[{"key":"movie-1","matches":[]}]}' },
      { status: '400 / 500', contentType: 'application/json', description: '输入或重复 key 无效 / 匹配查询失败，返回 error。' }, ...errors,
    ],
    notes: ['不按标题模糊匹配；仅查询已关联外部标识的普通 metadata，不自动合并 NFO、红果等独立资料体系。', '媒体项 ID 可继续交给普通 Emby 接口；不返回文件路径、播放地址或其他用户的播放状态。'],
  },
  {
    id: 'ext-tmdb-image', category: '扩展图片', name: '资料中心 TMDb 图片', methods: ['GET', 'HEAD'],
    path: '/emby-ext/v1/images/tmdb/:kind/:tmdbID/:type', auth: 'token', support: 'implemented',
    description: '使用 Emby 令牌读取资料中心图片；榜单中的相对图片 URL 已指向此入口。',
    parameters: [token,
      { name: 'kind', location: 'path', type: 'string', required: true, description: 'movie / series / tv / season / episode / person。' },
      { name: 'tmdbID', location: 'path', type: 'integer', required: true, description: '正整数来源 ID。' },
      { name: 'type', location: 'path', type: 'string', required: true, description: 'poster / backdrop / still / profile。' },
    ],
    responses: [
      { status: '200', contentType: 'image/*', description: '图片字节，HEAD 无正文；复用资料中心本地图片缓存。' },
      { status: '400 / 404 / 503', contentType: 'empty', description: '参数无效 / 图片缺失 / 图片服务暂不可用。' }, ...errors,
    ],
  },
]
