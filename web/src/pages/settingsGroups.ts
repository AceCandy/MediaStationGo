import { adultSettingsGroup } from './settingsGroupAccess'
import { generalSettingsGroup } from './settingsGroupGeneral'
import { playbackSettingsGroup } from './settingsGroupPlayback'
import { recognitionWordsSettingsGroup } from './settingsGroupRecognitionWords'
import type { SettingGroup } from './settingsGroupTypes'

export type { SettingGroup, SettingGroupKey } from './settingsGroupTypes'

export const GROUPS: SettingGroup[] = [
  generalSettingsGroup,
  playbackSettingsGroup,
  recognitionWordsSettingsGroup,
  adultSettingsGroup,
]

export const ALL_KEYS = new Set(
  GROUPS.flatMap((group) => [...group.items, ...(group.advancedItems ?? [])].map((item) => item.key)),
)
