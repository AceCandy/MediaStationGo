import type { SettingDef } from './SettingsRow'

export type SettingGroupKey = 'general' | 'playback' | 'recognition-words' | 'access'

export interface SettingGroup {
  key: SettingGroupKey
  label: string
  description?: string
  items: SettingDef[]
  advancedItems?: SettingDef[]
}
