export const EMBY_API_CATEGORIES = [
  '发现与认证',
  '用户与媒体库',
  '媒体项',
  '图片与播放',
  '播放状态',
  '会话',
] as const

export type EmbyApiCategory = (typeof EMBY_API_CATEGORIES)[number]
export type EmbyApiMethod = 'GET' | 'POST' | 'DELETE' | 'HEAD'
export type EmbyApiSupport = 'implemented' | 'compatibility'
export type EmbyApiParameterLocation = 'path' | 'query' | 'header' | 'body'

export type EmbyApiField = {
  name: string
  type: string
  description: string
}

export type EmbyApiParameter = EmbyApiField & {
  location: EmbyApiParameterLocation
  required?: boolean
}

export type EmbyApiResponse = {
  status: string
  contentType: string
  description: string
  fields?: readonly EmbyApiField[]
  example?: string
}

export type EmbyApiEndpoint = {
  id: string
  category: EmbyApiCategory
  name: string
  description: string
  methods: readonly EmbyApiMethod[]
  path: string
  aliases?: readonly string[]
  auth: 'public' | 'token'
  support: EmbyApiSupport
  parameters: readonly EmbyApiParameter[]
  requestExample?: string
  responses: readonly EmbyApiResponse[]
  notes?: readonly string[]
}

const tokenHeader: EmbyApiParameter = {
  name: 'X-Emby-Token',
  location: 'header',
  type: 'string',
  description: '推荐传递登录返回的访问令牌；也兼容 Authorization Bearer、MediaBrowser Token 和 api_key 查询参数。',
}

const userFields: readonly EmbyApiField[] = [
  { name: 'Id', type: 'string', description: '用户 ID。' },
  { name: 'Name', type: 'string', description: '用户名。' },
  { name: 'ServerId', type: 'string', description: '当前服务器 ID。' },
  { name: 'Policy', type: 'object', description: 'Emby 客户端使用的用户权限策略。' },
  { name: 'Configuration', type: 'object', description: '用户侧 Emby 配置。' },
]

const itemFields: readonly EmbyApiField[] = [
  { name: 'Id', type: 'string', description: '媒体项 ID。' },
  { name: 'Name', type: 'string', description: '标题。' },
  { name: 'Type', type: 'string', description: 'Movie、Series、Season、Episode 等 Emby 类型。' },
  { name: 'MediaType', type: 'string', description: 'Video 等媒体类型。' },
  { name: 'RunTimeTicks', type: 'number', description: '以 100ns 为单位的时长。' },
  { name: 'PartCount', type: 'number', description: '当前播放版本的物理 Part 数量；单文件省略。' },
  { name: 'ImageTags', type: 'object', description: '图片类型与缓存标识。' },
  { name: 'UserData', type: 'object', description: '收藏、已播放和进度等用户状态。' },
  { name: 'MediaSources', type: 'array', description: '可播放媒体源及流地址。' },
]

const itemsEnvelopeFields: readonly EmbyApiField[] = [
  { name: 'Items', type: 'array', description: '媒体项数组。' },
  { name: 'TotalRecordCount', type: 'number', description: '匹配总数。' },
  { name: 'StartIndex', type: 'number', description: '本次结果的起始位置，部分兼容响应会省略。' },
]

const itemsExample = `{
  "Items": [
    {
      "Id": "media-42",
      "Name": "示例影片",
      "Type": "Movie",
      "MediaType": "Video",
      "RunTimeTicks": 72000000000
    }
  ],
  "TotalRecordCount": 1,
  "StartIndex": 0
}`

const noContentResponse: EmbyApiResponse = {
  status: '204',
  contentType: '无响应体',
  description: '请求已处理。',
}

const playStateParameters: readonly EmbyApiParameter[] = [
  tokenHeader,
  { name: 'ItemId', location: 'body', type: 'string', description: '正在播放的媒体项 ID；也可通过同名 Query 传递。' },
  { name: 'MediaSourceId', location: 'body', type: 'string', description: 'PlaybackInfo 返回的媒体源 ID。' },
  { name: 'PlaySessionId', location: 'body', type: 'string', description: 'PlaybackInfo 返回的播放会话 ID；同一次播放必须复用。' },
  { name: 'PositionTicks', location: 'body', type: 'number', description: '当前播放位置。' },
  { name: 'RunTimeTicks', location: 'body', type: 'number', description: '媒体总时长；可省略，服务端会读取 ffprobe 时长，仍未知时接受请求但不记录进度。' },
]

const playStateRequest = `{
  "ItemId": "media-42",
  "MediaSourceId": "source-42",
  "PlaySessionId": "play-session-1",
  "PositionTicks": 18000000000,
  "RunTimeTicks": 72000000000
}`

const userDataFields: readonly EmbyApiField[] = [
  { name: 'IsFavorite', type: 'boolean', description: '是否收藏。' },
  { name: 'Played', type: 'boolean', description: '是否已播放。' },
  { name: 'PlaybackPositionTicks', type: 'number', description: '播放进度，存在用户数据时返回。' },
]

export const EMBY_API_ENDPOINTS: readonly EmbyApiEndpoint[] = [
  {
    id: 'system-info-public',
    category: '发现与认证',
    name: '公开服务器信息',
    description: '播放器发现服务器并判断 Emby 兼容版本。',
    methods: ['GET', 'HEAD'],
    path: '/System/Info/Public',
    aliases: ['/system/info/public', '/emby', '/emby/'],
    auth: 'public',
    support: 'implemented',
    parameters: [],
    responses: [{
      status: '200',
      contentType: 'application/json',
      description: '返回服务器标识、版本、访问地址和发现能力。HEAD 不返回响应体。',
      fields: [
        { name: 'Id / ServerId', type: 'string', description: '固定服务器标识 mediastation-go-001。' },
        { name: 'ServerName', type: 'string', description: '服务器名称。' },
        { name: 'Version / ServerVersion', type: 'string', description: '对外声明的 Emby 兼容版本。' },
        { name: 'LocalAddress / WanAddress', type: 'string', description: '根据当前请求 Host 生成的访问地址。' },
        { name: 'SupportsHttps', type: 'boolean', description: '是否声明 HTTPS 支持。' },
      ],
      example: `{
  "Id": "mediastation-go-001",
  "ServerId": "mediastation-go-001",
  "ServerName": "MediaStationGo",
  "Version": "4.8.10.0",
  "ProductName": "Emby Server",
  "LocalAddress": "https://media.example.test",
  "SupportsAutoDiscovery": true
}`,
    }],
    notes: ['除特殊说明外，目录中的路径同时支持直接路径和加 /emby 前缀。'],
  },
  {
    id: 'system-info',
    category: '发现与认证',
    name: '完整服务器信息',
    description: '返回播放器初始化所需的完整 Emby 服务器状态和能力信息。',
    methods: ['GET', 'HEAD'],
    path: '/System/Info',
    aliases: ['/system/info'],
    auth: 'public',
    support: 'implemented',
    parameters: [],
    responses: [{
      status: '200',
      contentType: 'application/json',
      description: '公开服务器信息的扩展结构；HEAD 不返回响应体。',
      fields: [
        { name: 'Id / ServerId / ServerName', type: 'string', description: '服务器标识与名称。' },
        { name: 'Version / ServerVersion', type: 'string', description: 'Emby 兼容版本。' },
        { name: 'Architecture / OperatingSystem', type: 'string', description: '对外声明的架构与操作系统。' },
        { name: 'LocalAddress / WanAddress', type: 'string', description: '根据当前请求生成的访问地址。' },
        { name: 'CanSelfRestart / CanLaunchWebBrowser', type: 'boolean', description: '服务器能力标记。' },
      ],
      example: `{
  "Id": "mediastation-go-001",
  "ServerName": "MediaStationGo",
  "Version": "4.8.10.0",
  "Architecture": "X64",
  "OperatingSystem": "Windows",
  "CanSelfRestart": false
}`,
    }],
  },
  {
    id: 'system-endpoint',
    category: '发现与认证',
    name: '网络位置判断',
    description: '兼容播放器判断当前客户端是否位于本地网络。',
    methods: ['GET'],
    path: '/System/Endpoint',
    aliases: ['/system/endpoint'],
    auth: 'public',
    support: 'compatibility',
    parameters: [],
    responses: [{
      status: '200',
      contentType: 'application/json',
      description: '当前固定声明为本地网络。',
      fields: [
        { name: 'IsLocal', type: 'boolean', description: '固定为 true。' },
        { name: 'IsInNetwork', type: 'boolean', description: '固定为 true。' },
      ],
      example: `{ "IsLocal": true, "IsInNetwork": true }`,
    }],
  },
  {
    id: 'system-ping',
    category: '发现与认证',
    name: '服务探活',
    description: '供客户端快速确认 Emby 兼容服务在线。',
    methods: ['GET', 'HEAD', 'POST'],
    path: '/System/Ping',
    aliases: ['/system/ping'],
    auth: 'public',
    support: 'compatibility',
    parameters: [],
    responses: [{ status: '200', contentType: 'text/plain', description: '固定返回 Emby Server；HEAD 不返回响应体。', example: 'Emby Server' }],
  },
  {
    id: 'public-users',
    category: '发现与认证',
    name: '公开用户列表',
    description: '登录页获取可见用户。',
    methods: ['GET'],
    path: '/Users/Public',
    aliases: ['/users/public'],
    auth: 'public',
    support: 'implemented',
    parameters: [],
    responses: [{
      status: '200',
      contentType: 'application/json',
      description: '返回精简用户数组；查询失败时兼容返回空数组。',
      fields: [
        { name: '[].Id', type: 'string', description: '用户 ID。' },
        { name: '[].Name', type: 'string', description: '用户名。' },
        { name: '[].ServerId', type: 'string', description: '服务器 ID。' },
        { name: '[].HasPassword', type: 'boolean', description: '当前固定为 true。' },
      ],
      example: `[
  { "Id": "user-1", "Name": "viewer", "ServerId": "mediastation-go-001", "HasPassword": true }
]`,
    }],
  },
  {
    id: 'authenticate-by-name',
    category: '发现与认证',
    name: '用户名密码登录',
    description: '使用 MediaStationGo 账户换取 Emby 访问令牌和会话信息。',
    methods: ['POST'],
    path: '/Users/AuthenticateByName',
    aliases: ['/Users/authenticatebyname', '/users/AuthenticateByName', '/users/authenticatebyname'],
    auth: 'public',
    support: 'implemented',
    parameters: [
      { name: 'Username', location: 'body', type: 'string', required: true, description: '用户名；兼容 Name。' },
      { name: 'Pw', location: 'body', type: 'string', required: true, description: '明文密码；兼容 Password，不接受仅 MD5/SHA1。' },
      { name: 'X-Emby-Authorization', location: 'header', type: 'string', description: '可包含 Client、Device、DeviceId、Version 等客户端信息。' },
    ],
    requestExample: `{
  "Username": "viewer",
  "Pw": "example-password"
}`,
    responses: [
      {
        status: '200',
        contentType: 'application/json',
        description: '登录成功并创建兼容会话。',
        fields: [
          { name: 'AccessToken', type: 'string', description: '有效期 30 天的 Emby JWT。' },
          { name: 'ServerId', type: 'string', description: '服务器 ID。' },
          { name: 'User', type: 'object', description: '完整用户对象。' },
          { name: 'SessionInfo', type: 'object', description: '会话、客户端和设备信息。' },
        ],
        example: `{
  "AccessToken": "example.jwt.token",
  "ServerId": "mediastation-go-001",
  "User": { "Id": "user-1", "Name": "viewer" },
  "SessionInfo": { "Id": "session-1", "UserId": "user-1", "Client": "ExamplePlayer" }
}`,
      },
      { status: '400 / 401', contentType: 'application/json', description: '参数不完整、仅提供哈希密码或账户认证失败。', example: `{ "Code": 401, "Message": "invalid credentials" }` },
    ],
    notes: ['同时兼容 JSON、表单和 Query 入参；推荐使用 JSON Body。', '每个来源 IP 限制为每分钟 30 次。'],
  },
  {
    id: 'session-capabilities',
    category: '发现与认证',
    name: '上报播放器能力',
    description: '兼容播放器登录前后上报会话能力的探测请求。',
    methods: ['POST'],
    path: '/Sessions/Capabilities',
    aliases: ['/Sessions/Capabilities/Full', '/sessions/capabilities', '/sessions/capabilities/full'],
    auth: 'public',
    support: 'compatibility',
    parameters: [],
    responses: [noContentResponse],
    notes: ['当前不解析能力 Body；携带有效普通 Token 时会记录会话活动。'],
  },
  {
    id: 'session-logout',
    category: '发现与认证',
    name: '退出播放器会话',
    description: '按可识别的用户、设备和来源地址结束兼容会话。',
    methods: ['POST'],
    path: '/Sessions/Logout',
    aliases: ['/sessions/logout'],
    auth: 'public',
    support: 'implemented',
    parameters: [{ ...tokenHeader, required: false }],
    responses: [noContentResponse],
    notes: ['接口不强制 Token；无法识别会话时也返回 204。', '退出会话不会撤销已签发 JWT。'],
  },
  {
    id: 'users-me',
    category: '用户与媒体库',
    name: '当前用户',
    description: '读取当前令牌对应的 Emby 用户资料和策略。',
    methods: ['GET'],
    path: '/Users/Me',
    aliases: ['/users/me'],
    auth: 'token',
    support: 'implemented',
    parameters: [tokenHeader],
    responses: [
      { status: '200', contentType: 'application/json', description: '返回当前用户对象。', fields: userFields, example: `{ "Id": "user-1", "Name": "viewer", "ServerId": "mediastation-go-001", "Policy": {} }` },
      { status: '401 / 404', contentType: 'application/json', description: '令牌无效或用户不存在。' },
    ],
  },
  {
    id: 'user-views',
    category: '用户与媒体库',
    name: '用户媒体视图',
    description: '返回当前用户可访问的媒体库视图。',
    methods: ['GET'],
    path: '/Users/:userId/Views',
    aliases: ['/users/:userId/views'],
    auth: 'token',
    support: 'implemented',
    parameters: [
      tokenHeader,
      { name: 'userId', location: 'path', type: 'string', required: true, description: '必须与令牌用户一致；管理员可显式指定其他账户。' },
    ],
    responses: [{ status: '200', contentType: 'application/json', description: '返回 CollectionFolder 类型的 Items 分页结构；设置封面的媒体库包含 ImageTags.Primary，封面地址变化时更新缓存标识。', fields: itemsEnvelopeFields, example: `{
  "Items": [{ "Id": "library-1", "Name": "电影", "Type": "CollectionFolder", "CollectionType": "movies" }],
  "TotalRecordCount": 1,
  "StartIndex": 0
}` }],
  },
  {
    id: 'library-media-folders',
    category: '用户与媒体库',
    name: '媒体文件夹',
    description: '返回当前用户可访问的媒体库文件夹，行为与用户视图一致。',
    methods: ['GET'],
    path: '/Library/MediaFolders',
    aliases: ['/library/mediafolders'],
    auth: 'token',
    support: 'implemented',
    parameters: [tokenHeader],
    responses: [{ status: '200', contentType: 'application/json', description: '媒体库 Items 分页结构。', fields: itemsEnvelopeFields, example: `{
  "Items": [{ "Id": "library-1", "Name": "电影", "CollectionType": "movies" }],
  "TotalRecordCount": 1,
  "StartIndex": 0
}` }],
  },
  {
    id: 'library-virtual-folders',
    category: '用户与媒体库',
    name: '虚拟媒体库列表',
    description: '返回当前用户可访问的虚拟媒体库配置数组。',
    methods: ['GET'],
    path: '/Library/VirtualFolders',
    aliases: ['/Library/SelectableMediaFolders', '/library/virtualfolders', '/library/selectablemediafolders'],
    auth: 'token',
    support: 'implemented',
    parameters: [tokenHeader],
    responses: [{
      status: '200',
      contentType: 'application/json',
      description: '直接返回媒体库数组，不使用 Items 分页结构。',
      fields: [
        { name: '[].Name / CollectionType', type: 'string', description: '媒体库名称和集合类型。' },
        { name: '[].Locations', type: 'array', description: '媒体库位置列表。' },
        { name: '[].ItemId / Id', type: 'string', description: '媒体库标识。' },
        { name: '[].LibraryOptions', type: 'object', description: '兼容播放器读取的媒体库选项。' },
      ],
      example: `[{ "Id": "library-1", "ItemId": "library-1", "Name": "电影", "CollectionType": "movies", "Locations": [], "LibraryOptions": {} }]`,
    }],
  },
  {
    id: 'items-query',
    category: '媒体项',
    name: '查询媒体项',
    description: '按媒体库、类型、关键字和用户状态查询 Emby 媒体项。',
    methods: ['GET'],
    path: '/Items',
    aliases: ['/Users/:userId/Items', '/items', '/users/:userId/items'],
    auth: 'token',
    support: 'implemented',
    parameters: [
      tokenHeader,
      { name: 'userId', location: 'path', type: 'string', description: '必须与令牌用户一致；管理员可显式指定其他账户。' },
      { name: 'ParentId', location: 'query', type: 'string', description: '父级媒体库、剧集或季 ID。' },
      { name: 'Ids', location: 'query', type: 'string', description: '逗号分隔的媒体 ID。' },
      { name: 'PersonIds', location: 'query', type: 'string', description: '逗号分隔的人物 ID，仅返回这些人物参与的作品。' },
      { name: 'SearchTerm', location: 'query', type: 'string', description: '搜索顶层 Movie/Series 的标题与原名及 Person 姓名；所有关键词均需命中，Movie/Series 中 0–100 的阿拉伯数字与标准中文数字可双向匹配；兼容播放器为单字符自动追加的 %；统一排序后最多返回 100 条。' },
      { name: 'IncludeItemTypes', location: 'query', type: 'string', description: '非空搜索时 Person、Movie、Series 按 OR 合并；人物仅按姓名返回，不展开参演作品；混合请求忽略 MusicAlbum 等未支持类型，全部不支持时返回空结果。无搜索词的普通浏览不合并人物。' },
      { name: 'Filters', location: 'query', type: 'string', description: '逗号分隔的过滤条件；IsFavorite 仅支持 Movie 和 Series。' },
      { name: 'Fields', location: 'query', type: 'string', description: '可选字段列表；指定后仅按需返回 People、ProviderIds 和 MediaSources，省略时保持完整兼容响应。' },
      { name: 'Recursive', location: 'query', type: 'boolean', description: '是否递归查询。' },
      { name: 'SortBy / SortOrder', location: 'query', type: 'string', description: '排序字段和方向。' },
      { name: 'Limit / StartIndex', location: 'query', type: 'number', description: '按顶层 Metadata 分页，Limit 默认 50，最大 500；非空搜索在最多 100 条候选内分页。' },
    ],
    responses: [{ status: '200', contentType: 'application/json', description: '媒体项分页结构。', fields: itemsEnvelopeFields, example: itemsExample }],
  },
  {
    id: 'search-hints',
    category: '媒体项',
    name: '搜索提示',
    description: '返回播放器全局搜索使用的轻量媒体命中结果。',
    methods: ['GET'],
    path: '/Search/Hints',
    aliases: ['/Users/:userId/Search/Hints', '/SearchHints', '/Users/:userId/SearchHints', '/search/hints', '/searchhints', '/users/:userId/search/hints', '/users/:userId/searchhints'],
    auth: 'token',
    support: 'implemented',
    parameters: [
      tokenHeader,
      { name: 'SearchTerm', location: 'query', type: 'string', required: true, description: '搜索顶层 Movie/Series 的标题与原名及 Person 姓名；所有关键词均需命中，Movie/Series 中 0–100 的阿拉伯数字与标准中文数字可双向匹配；兼容播放器为单字符自动追加的 %；统一排序后最多返回 100 条，为空时返回空数组。' },
      { name: 'IncludeItemTypes', location: 'query', type: 'string', description: 'Person、Movie、Series 按 OR 合并；人物不展开参演作品；未支持类型在混合请求中忽略，全部不支持时返回空结果。' },
      { name: 'Limit / StartIndex', location: 'query', type: 'number', description: '在最多 100 条搜索候选内按顶层 Metadata 分页。' },
    ],
    responses: [{
      status: '200',
      contentType: 'application/json',
      description: '搜索提示分页结构。',
      fields: [
        { name: 'SearchHints', type: 'array', description: '轻量搜索结果。' },
        { name: 'SearchHints[].ItemId / Id', type: 'string', description: '媒体项 ID。' },
        { name: 'SearchHints[].Name / Type', type: 'string', description: '名称和 Emby 类型。' },
        { name: 'TotalRecordCount / StartIndex', type: 'number', description: '分页信息。' },
      ],
      example: `{
  "SearchHints": [{ "ItemId": "media-42", "Id": "media-42", "Name": "示例影片", "Type": "Movie" }],
  "TotalRecordCount": 1,
  "StartIndex": 0
}`,
    }],
  },
  {
    id: 'item-detail',
    category: '媒体项',
    name: '媒体项详情',
    description: '读取影片、剧集、季或单集详情及其播放信息。',
    methods: ['GET'],
    path: '/Items/:id',
    aliases: ['/Users/:userId/Items/:id', '/items/:id'],
    auth: 'token',
    support: 'implemented',
    parameters: [
      tokenHeader,
      { name: 'id', location: 'path', type: 'string', required: true, description: '媒体项 ID。' },
      { name: 'userId', location: 'path', type: 'string', description: '必须与令牌用户一致；管理员可显式指定其他账户。' },
    ],
    responses: [
      { status: '200', contentType: 'application/json', description: '完整 Emby 媒体项。', fields: itemFields, example: `{
  "Id": "media-42",
  "Name": "示例影片",
  "Type": "Movie",
  "MediaType": "Video",
  "RunTimeTicks": 72000000000,
  "UserData": { "IsFavorite": false, "Played": false }
}` },
      { status: '404', contentType: 'application/json', description: '媒体项不存在。' },
    ],
  },
  {
    id: 'additional-parts',
    category: '媒体项',
    name: '媒体附加 Part',
    description: '返回当前首选播放版本除首 Part 外的物理文件；每项使用自己的媒体源 ID、时长、轨道和直接播放地址。',
    methods: ['GET'],
    path: '/Videos/:id/AdditionalParts',
    aliases: ['/videos/:id/additionalparts'],
    auth: 'token',
    support: 'implemented',
    parameters: [
      tokenHeader,
      { name: 'id', location: 'path', type: 'string', required: true, description: '媒体项 ID 或具体媒体源 ID。' },
    ],
    responses: [{
      status: '200',
      contentType: 'application/json',
      description: '按 Part 顺序返回后续物理文件；无附加 Part 时 Items 为空。',
      fields: itemsEnvelopeFields,
      example: `{
  "Items": [{
    "Id": "media-part-2",
    "Name": "示例影片",
    "Type": "Movie",
    "MediaSources": [{
      "Id": "media-part-2",
      "DirectStreamUrl": "/Videos/media-part-2/stream.mkv?api_key=example.jwt.token"
    }]
  }],
  "TotalRecordCount": 1
}`,
    }],
  },
  {
    id: 'items-latest',
    category: '媒体项',
    name: '最近入库',
    description: '按逻辑作品归组后返回最近入库的媒体项，默认隐藏已播放完成的作品；剧集分集归到 Series，并按可见版本的最新入库时间排序。',
    methods: ['GET'],
    path: '/Items/Latest',
    aliases: ['/Users/:userId/Items/Latest', '/items/latest'],
    auth: 'token',
    support: 'implemented',
    parameters: [
      tokenHeader,
      { name: 'ParentId', location: 'query', type: 'string', description: '限制到指定媒体库。' },
      { name: 'Limit', location: 'query', type: 'number', description: '返回数量，默认 20，最大 100。' },
      { name: 'IsPlayed', location: 'query', type: 'boolean', description: '默认 false；true 只返回已播放完成作品，false 只返回未播放完成作品。' },
    ],
    responses: [{ status: '200', contentType: 'application/json', description: '媒体项数组，不使用分页 envelope。', fields: itemFields, example: `[{ "Id": "media-42", "Name": "示例影片", "Type": "Movie" }]` }],
  },
  {
    id: 'items-resume',
    category: '媒体项',
    name: '继续观看',
    description: '返回有进度且尚未播放完成的媒体项。',
    methods: ['GET'],
    path: '/Items/Resume',
    aliases: ['/Users/:userId/Items/Resume', '/items/resume'],
    auth: 'token',
    support: 'implemented',
    parameters: [tokenHeader, { name: 'Limit', location: 'query', type: 'number', description: '返回数量，默认 20，最大 100。' }],
    responses: [{ status: '200', contentType: 'application/json', description: '继续观看 Items 结构。', fields: itemsEnvelopeFields, example: itemsExample }],
  },
  {
    id: 'show-seasons',
    category: '媒体项',
    name: '剧集季列表',
    description: '读取指定剧集下的季。',
    methods: ['GET'],
    path: '/Shows/:id/Seasons',
    aliases: ['/Users/:userId/Shows/:id/Seasons', '/shows/:id/seasons'],
    auth: 'token',
    support: 'implemented',
    parameters: [tokenHeader, { name: 'id', location: 'path', type: 'string', required: true, description: 'Series ID。' }],
    responses: [{ status: '200', contentType: 'application/json', description: 'Season 类型的 Items 结构。', fields: itemsEnvelopeFields, example: itemsExample }],
  },
  {
    id: 'show-episodes',
    category: '媒体项',
    name: '剧集单集列表',
    description: '读取指定剧集或季下的单集。',
    methods: ['GET'],
    path: '/Shows/:id/Episodes',
    aliases: ['/Users/:userId/Shows/:id/Episodes', '/shows/:id/episodes'],
    auth: 'token',
    support: 'implemented',
    parameters: [
      tokenHeader,
      { name: 'id', location: 'path', type: 'string', required: true, description: 'Series 或 Season ID。' },
      { name: 'SeasonId', location: 'query', type: 'string', description: '指定季 ID；存在时优先于 path id。' },
    ],
    responses: [{ status: '200', contentType: 'application/json', description: 'Episode 类型的 Items 结构。', fields: itemsEnvelopeFields, example: itemsExample }],
  },
  {
    id: 'empty-items-compatibility',
    category: '媒体项',
    name: '空列表兼容端点',
    description: '满足播放器探测，但当前不提供对应业务数据。',
    methods: ['GET'],
    path: '/Shows/NextUp',
    aliases: ['/Users/:userId/Shows/NextUp', '/MediaSegments/:id', '/Artists', '/Genres', '/Shows/Upcoming', '/Items/:id/Similar', '/Items/:id/ThumbnailSet', '/Items/:id/Intros', '/Items/:id/SpecialFeatures'],
    auth: 'token',
    support: 'compatibility',
    parameters: [tokenHeader],
    responses: [{ status: '200', contentType: 'application/json', description: '固定空 Items 结构。', fields: itemsEnvelopeFields, example: `{ "Items": [], "TotalRecordCount": 0 }` }],
    notes: ['这些端点存在并可被客户端调用，但不能视为已实现对应推荐、类型或附加内容能力。'],
  },
  {
    id: 'item-image',
    category: '图片与播放',
    name: '媒体图片',
    description: '读取封面、背景图、人物图等媒体图片；媒体库 Primary 支持上传封面和外链封面。',
    methods: ['GET', 'HEAD'],
    path: '/Items/:id/Images/:type',
    aliases: ['/Items/:id/Images/:type/:index', '/items/:id/images/:type', '/items/:id/images/:type/:index'],
    auth: 'public',
    support: 'implemented',
    parameters: [
      { name: 'id', location: 'path', type: 'string', required: true, description: '媒体库、媒体项或人物 ID。' },
      { name: 'type', location: 'path', type: 'string', required: true, description: 'Primary、Backdrop 等图片类型。' },
      { name: 'index', location: 'path', type: 'number', description: '多图类型的索引。' },
    ],
    responses: [{ status: '200', contentType: 'image/*', description: '返回实际图片；图片不可用时返回缓存一小时的占位 PNG。HEAD 仅返回响应头。' }],
  },
  {
    id: 'playback-info-get',
    category: '图片与播放',
    name: '获取播放信息',
    description: '为播放器选择媒体源、音轨和字幕轨，生成直接播放地址。',
    methods: ['GET'],
    path: '/Items/:id/PlaybackInfo',
    aliases: ['/Users/:userId/Items/:id/PlaybackInfo', '/items/:id/playbackinfo'],
    auth: 'token',
    support: 'implemented',
    parameters: [
      tokenHeader,
      { name: 'id', location: 'path', type: 'string', required: true, description: '媒体项 ID。' },
      { name: 'MediaSourceId', location: 'query', type: 'string', description: '指定媒体源。' },
      { name: 'AudioStreamIndex', location: 'query', type: 'number', description: '音轨索引，-1 表示不指定。' },
      { name: 'SubtitleStreamIndex', location: 'query', type: 'number', description: '字幕轨索引，-1 表示关闭字幕。' },
    ],
    responses: [{
      status: '200',
      contentType: 'application/json',
      description: '播放会话和媒体源信息。',
      fields: [
        { name: 'MediaSources', type: 'array', description: '媒体源、容器、流和 DirectStreamUrl。' },
        { name: 'MediaSources[].MediaStreams', type: 'array', description: '视频、音频和字幕轨。' },
        { name: 'PlaySessionId', type: 'string', description: '本次播放会话 ID。' },
        { name: 'DateCreated', type: 'string', description: '播放信息生成时间。' },
      ],
      example: `{
  "MediaSources": [{
    "Id": "source-42",
    "Container": "mkv",
    "SupportsDirectPlay": true,
    "DirectStreamUrl": "/Videos/media-42/stream.mkv?api_key=example.jwt.token",
    "MediaStreams": []
  }],
  "PlaySessionId": "play-1",
  "DateCreated": "2026-01-01T00:00:00Z"
}`,
    }, { status: '400 / 404', contentType: 'application/json', description: '轨道索引非法或媒体不可播放。' }],
  },
  {
    id: 'playback-info-post',
    category: '图片与播放',
    name: '提交播放选择',
    description: '使用 JSON Body 指定媒体源、用户、音轨和字幕轨。',
    methods: ['POST'],
    path: '/Items/:id/PlaybackInfo',
    aliases: ['/Users/:userId/Items/:id/PlaybackInfo', '/items/:id/playbackinfo'],
    auth: 'token',
    support: 'implemented',
    parameters: [
      tokenHeader,
      { name: 'id', location: 'path', type: 'string', required: true, description: '媒体项 ID。' },
      { name: 'MediaSourceId', location: 'body', type: 'string', description: '指定媒体源。' },
      { name: 'AudioStreamIndex', location: 'body', type: 'number', description: '音轨索引。' },
      { name: 'SubtitleStreamIndex', location: 'body', type: 'number', description: '字幕轨索引。' },
      { name: 'UserId', location: 'body', type: 'string', description: '用户 ID；必须与令牌用户一致，管理员除外。' },
    ],
    requestExample: `{
  "MediaSourceId": "source-42",
  "AudioStreamIndex": 1,
  "SubtitleStreamIndex": 2,
  "UserId": "user-1"
}`,
    responses: [{ status: '200', contentType: 'application/json', description: '响应结构与 GET PlaybackInfo 相同。', fields: [{ name: 'MediaSources', type: 'array', description: '媒体源与已选择轨道。' }, { name: 'PlaySessionId', type: 'string', description: '播放会话 ID。' }], example: `{ "MediaSources": [{ "Id": "source-42" }], "PlaySessionId": "play-1" }` }],
  },
  {
    id: 'video-stream',
    category: '图片与播放',
    name: '视频原始流',
    description: '直接读取本地媒体文件，或按底层流服务行为处理远程媒体。',
    methods: ['GET', 'HEAD'],
    path: '/Videos/:id/stream.:container',
    aliases: ['/Videos/:id/stream', '/Videos/:id/original', '/Videos/:id/original.:container', '/videos/:id/stream', '/videos/:id/stream.:container', '/videos/:id/original', '/videos/:id/original.:container'],
    auth: 'token',
    support: 'implemented',
    parameters: [
      tokenHeader,
      { name: 'id', location: 'path', type: 'string', required: true, description: '具体媒体源 ID，即 PlaybackInfo MediaSources[].Id；不接受 metadata、season 或 series ID。' },
      { name: 'container', location: 'path', type: 'string', description: '客户端期望的容器扩展名。' },
    ],
    responses: [
      { status: '200 / 206', contentType: 'video/* 或 application/octet-stream', description: '返回媒体内容；范围请求由流服务处理。HEAD 仅返回响应头。' },
      { status: '302', contentType: '无响应体', description: 'STRM 或远程媒体可能重定向到外部播放地址。' },
      { status: '404 / 500 / 502', contentType: 'application/json', description: '具体媒体不存在，或媒体查询、底层流服务失败。' },
      { status: '499', contentType: '无响应体', description: '客户端在播放流处理完成前取消请求。' },
    ],
  },
  {
    id: 'strm-stream-alias',
    category: '图片与播放',
    name: 'STRM 播放别名',
    description: '为 MediaStationGo 生成的 STRM 文件提供稳定播放入口。',
    methods: ['GET', 'HEAD'],
    path: '/emby/api/stream/:id',
    auth: 'token',
    support: 'implemented',
    parameters: [tokenHeader, { name: 'id', location: 'path', type: 'string', required: true, description: '具体媒体源 ID，即 PlaybackInfo MediaSources[].Id。' }],
    responses: [
      { status: '200 / 206', contentType: '媒体内容', description: '本地文件由流服务输出。' },
      { status: '302', contentType: '无响应体', description: '远程 STRM 地址重定向。' },
    ],
    notes: ['这是唯一只注册在 /emby 前缀下的播放别名，不存在无前缀 /api/stream/:id。'],
  },
  {
    id: 'subtitle-stream',
    category: '图片与播放',
    name: 'WebVTT 字幕流',
    description: '按 PlaybackInfo 中的字幕轨索引输出 WebVTT 字幕。',
    methods: ['GET'],
    path: '/Videos/:id/Subtitles/:index/Stream.vtt',
    aliases: ['/videos/:id/subtitles/:index/stream.vtt'],
    auth: 'token',
    support: 'implemented',
    parameters: [
      tokenHeader,
      { name: 'id', location: 'path', type: 'string', required: true, description: '媒体项 ID。' },
      { name: 'index', location: 'path', type: 'number', required: true, description: '非负字幕轨索引。' },
    ],
    responses: [
      { status: '200', contentType: 'text/vtt; charset=utf-8', description: 'WebVTT 字幕，Cache-Control 为 no-store。', example: `WEBVTT\n\n00:00:01.000 --> 00:00:04.000\n示例字幕` },
      { status: '404', contentType: '无固定响应体', description: '媒体不可播放、索引无效或字幕读取失败。' },
    ],
  },
  {
    id: 'session-playing',
    category: '播放状态',
    name: '开始播放',
    description: '记录播放开始并更新会话与设备活动。',
    methods: ['POST'],
    path: '/Sessions/Playing',
    aliases: ['/sessions/playing'],
    auth: 'token',
    support: 'implemented',
    parameters: playStateParameters,
    requestExample: playStateRequest,
    responses: [noContentResponse, { status: '200', contentType: '无响应体', description: '兼容空 ItemId 请求。' }],
  },
  {
    id: 'session-progress',
    category: '播放状态',
    name: '回传播放进度',
    description: '更新用户播放位置、会话和设备状态。',
    methods: ['POST'],
    path: '/Sessions/Playing/Progress',
    aliases: ['/sessions/playing/progress'],
    auth: 'token',
    support: 'implemented',
    parameters: playStateParameters,
    requestExample: playStateRequest,
    responses: [noContentResponse, { status: '400 / 401', contentType: 'application/json', description: '进度参数非法，或用户已被终止会话。' }],
  },
  {
    id: 'session-stopped',
    category: '播放状态',
    name: '结束播放',
    description: '保存最终进度并将会话标记为停止。',
    methods: ['POST'],
    path: '/Sessions/Playing/Stopped',
    aliases: ['/sessions/playing/stopped'],
    auth: 'token',
    support: 'implemented',
    parameters: playStateParameters,
    requestExample: playStateRequest,
    responses: [noContentResponse],
  },
  {
    id: 'favorite-item',
    category: '播放状态',
    name: '设置收藏状态',
    description: 'POST 收藏媒体项，DELETE 取消收藏。',
    methods: ['POST', 'DELETE'],
    path: '/Users/:userId/FavoriteItems/:itemId',
    aliases: ['/users/:userId/favoriteitems/:itemId'],
    auth: 'token',
    support: 'implemented',
    parameters: [
      tokenHeader,
      { name: 'userId', location: 'path', type: 'string', required: true, description: '必须与令牌用户一致；管理员可显式指定其他账户。' },
      { name: 'itemId', location: 'path', type: 'string', required: true, description: '媒体项 ID。' },
    ],
    responses: [{ status: '200', contentType: 'application/json', description: '返回更新后的 UserData；媒体无完整用户数据时至少返回 IsFavorite。', fields: userDataFields, example: `{ "IsFavorite": true }` }],
  },
  {
    id: 'played-item',
    category: '播放状态',
    name: '设置已播放状态',
    description: 'POST 标记已播放，DELETE 取消已播放。整剧和季递归更新当前用户可见且有文件的单集；剧/季状态按单集汇总，不预先标记缺失或未来入库的集。',
    methods: ['POST', 'DELETE'],
    path: '/Users/:userId/PlayedItems/:itemId',
    aliases: ['/users/:userId/playeditems/:itemId'],
    auth: 'token',
    support: 'implemented',
    parameters: [
      tokenHeader,
      { name: 'userId', location: 'path', type: 'string', required: true, description: '必须与令牌用户一致；管理员可显式指定其他账户。' },
      { name: 'itemId', location: 'path', type: 'string', required: true, description: '媒体项 ID。' },
    ],
    responses: [{ status: '200', contentType: 'application/json', description: '返回更新后的 UserData；媒体无完整用户数据时至少返回 Played。', fields: userDataFields, example: `{ "Played": true }` }],
  },
  {
    id: 'sessions-list',
    category: '会话',
    name: '播放器会话列表',
    description: '读取当前服务器上已知的 Emby 客户端和播放状态。',
    methods: ['GET'],
    path: '/Sessions',
    aliases: ['/sessions'],
    auth: 'token',
    support: 'implemented',
    parameters: [tokenHeader],
    responses: [{
      status: '200',
      contentType: 'application/json',
      description: '会话数组；会话服务不可用时返回空数组。',
      fields: [
        { name: '[].Id / Client', type: 'string', description: '会话 ID 和客户端名称。' },
        { name: '[].DeviceId / DeviceName', type: 'string', description: '设备标识和名称。' },
        { name: '[].UserId / UserName', type: 'string', description: '当前用户。' },
        { name: '[].PlayState', type: 'object', description: '位置、暂停状态、DirectStream 和可 Seek 标记。' },
        { name: '[].NowPlayingItem', type: 'object', description: '正在播放时返回媒体项 ID。' },
      ],
      example: `[
  {
    "Id": "session-1",
    "Client": "ExamplePlayer",
    "DeviceId": "device-1",
    "UserId": "user-1",
    "PlayState": { "PositionTicks": 18000000000, "IsPaused": false, "PlayMethod": "DirectStream", "CanSeek": true },
    "NowPlayingItem": { "Id": "media-42" }
  }
]`,
    }],
  },
]
