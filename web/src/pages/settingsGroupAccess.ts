import type { SettingGroup } from './settingsGroupTypes'

export const adultSettingsGroup: SettingGroup = {
  key: 'access',
  label: '内容访问',
  description: '这里设置全局成人内容策略；每个用户和观影 Profile 仍可在全局允许范围内设置个人偏好。',
  items: [
    {
      key: 'adult.enabled',
      label: '启用成人内容',
      type: 'toggle',
      hint: '全局开关。关闭后所有人都无法显示成人库；开启后用户仍默认隐藏，可在个人资料或 Bot 中自行显示。',
      defaultValue: 'true',
    },
    {
      key: 'adult.library_ids',
      label: '指定成人媒体库',
      type: 'library-multiselect',
      hint: '管理员指定哪些媒体库目录属于成人影视库。指定后网页、搜索和第三方客户端都会统一隐藏。',
      defaultValue: '[]',
    },
  ],
  advancedItems: [
    {
      key: 'adult.require_pin',
      label: '访问需要 PIN',
      type: 'toggle',
    },
    {
      key: 'adult.pin',
      label: 'PIN 码',
      type: 'text',
      hint: '4-8 位数字',
    },
  ],
}
