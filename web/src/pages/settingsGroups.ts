import { adultSettingsGroup } from './settingsGroupAccess'
import { generalSettingsGroup } from './settingsGroupGeneral'
import { playbackSettingsGroup } from './settingsGroupPlayback'
import { recognitionWordsSettingsGroup } from './settingsGroupRecognitionWords'
import type { SettingGroup } from './settingsGroupTypes'

export type { SettingGroup, SettingGroupKey } from './settingsGroupTypes'

export const GROUPS: SettingGroup[] = [
  generalSettingsGroup,
  {
    key: 'watching',
    label: '播放设置',
    description: '配置播放完成后的观看状态。',
    items: [{
      key: 'playback.auto_mark_previous_episodes',
      label: '自动标记本季前面的集数',
      type: 'toggle',
      defaultValue: 'false',
      hint: '某集播放达到已看完标准时，将当前用户同一季中前面未看完、可访问且已有文件的集数标为已观看。适用于网页和 Emby 播放；开启时不会追溯旧记录。',
    }],
  },
  playbackSettingsGroup,
  recognitionWordsSettingsGroup,
  adultSettingsGroup,
]

export const ALL_KEYS = new Set(
  GROUPS.flatMap((group) => [...group.items, ...(group.advancedItems ?? [])].map((item) => item.key)),
)
