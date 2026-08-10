import type { SettingGroup } from './settingsGroupTypes'

export const generalSettingsGroup: SettingGroup = {
  key: 'general',
  label: '常规',
  description: '语言 / 媒体探测参数（API 密钥请在管理后台 → 外部API 配置）',
  items: [
    {
      key: 'ui.hide_community_links_for_users',
      label: '对普通用户隐藏社区页脚链接',
      type: 'toggle',
      hint: '开启后，登录页及普通用户登录后的页脚不显示 TG 群组、开源仓库和作者主页；管理员仍可见。',
      defaultValue: 'false',
    },
    {
      key: 'tmdb.language',
      label: 'TMDb 元数据语言',
      type: 'select',
      options: [
        { value: 'zh-CN', label: '简体中文' },
        { value: 'zh-TW', label: '繁体中文' },
        { value: 'en-US', label: 'English' },
        { value: 'ja-JP', label: '日本語' },
      ],
    },
    {
      key: 'metadata.people_ai_translate',
      label: 'AI 翻译人物与角色名',
      type: 'toggle',
      hint: '开启后，刮削完成时使用已配置的 AI 将非中文人物名和演员角色名翻译为简体中文；默认关闭，避免产生额外调用费用。',
      defaultValue: 'false',
    },
    {
      key: 'app.server_url',
      label: '公开访问域名 / STRM 域名',
      type: 'text',
      hint: '例如 http://NAS-IP:18080 或 https://media.example.com。用于生成可供播放器访问的完整 STRM 地址；不填则使用同源相对路径。',
      placeholder: 'http://192.168.1.125:18080',
    },
    {
      key: 'playback.path_mappings',
      label: '本地播放路径 302 映射',
      type: 'textarea',
      hint: '每行一条“本地路径前缀 => 远程 HTTP URL 前缀”；命中时不读取本地文件，直接返回 302。更具体的前缀优先。',
      placeholder: '/mnt/media/archive/ => https://media.example.com/archive/',
    },
    {
      key: 'playback.redirect_resolve_prefixes',
      label: '播放直链 302 预解析前缀',
      type: 'textarea',
      hint: '每行一个 HTTP/HTTPS URL 前缀；STRM 地址或路径映射后的 URL 命中时，服务端读取第一跳 302 并缓存 1 小时，失败则返回原地址。',
      placeholder: 'http://media-gateway.example/d',
    },
    {
      key: 'ffprobe.path',
      label: 'FFprobe 路径',
      type: 'text',
      placeholder: 'ffprobe',
    },
    {
      key: 'ffprobe.max_concurrent',
      label: 'FFprobe 最大并发',
      type: 'number',
      hint: 'NAS 建议 1；用于扫描、整理洗版和手动探测，避免同时启动多个 ffprobe 进程',
      defaultValue: '1',
    },
  ],
}
