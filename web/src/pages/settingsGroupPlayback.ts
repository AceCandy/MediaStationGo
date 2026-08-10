import type { SettingGroup } from './settingsGroupTypes'

export const playbackSettingsGroup: SettingGroup = {
  key: 'playback',
  label: '播放与探测',
  description: '配置 STRM 公开地址、播放路径转换和媒体探测工具。',
  items: [
    {
      key: 'app.server_url',
      label: '公开访问域名 / STRM 域名',
      type: 'text',
      hint: '例如 http://NAS-IP:18080 或 https://media.example.com。用于生成可供播放器访问的完整 STRM 地址；不填则使用同源相对路径。',
      placeholder: 'http://192.168.1.125:18080',
    },
  ],
  advancedItems: [
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
      hint: 'NAS 建议 1；用于扫描、整理洗版和手动探测，避免同时启动多个 ffprobe 进程。',
      defaultValue: '1',
    },
  ],
}
