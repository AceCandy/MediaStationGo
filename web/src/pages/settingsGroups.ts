import { adultSettingsGroup } from './settingsGroupAccess'
import { cloudUploadSettingsGroup } from './settingsGroupCloud'
import { generalSettingsGroup, licenseSettingsGroup } from './settingsGroupGeneral'
import { subscriptionSettingsGroup } from './settingsGroupSubscriptions'
import type { SettingGroup } from './settingsGroupTypes'

export type { SettingGroup } from './settingsGroupTypes'

export const GROUPS: SettingGroup[] = [
  generalSettingsGroup,
  licenseSettingsGroup,
  subscriptionSettingsGroup,
  cloudUploadSettingsGroup,
  adultSettingsGroup,
]

export const ALL_KEYS = new Set(GROUPS.flatMap((group) => group.items.map((item) => item.key)))
