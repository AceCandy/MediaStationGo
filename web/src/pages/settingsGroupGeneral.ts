import type { SettingGroup } from './settingsGroupTypes'

export const generalSettingsGroup: SettingGroup = {
  key: 'general',
  label: '常规',
  description: '界面与元数据的常用配置。外部 API 密钥请在“用户与集成 → 外部 API”中管理。',
  items: [
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
  ],
}
