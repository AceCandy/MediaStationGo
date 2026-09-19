import { RefreshCw } from 'lucide-react'

import {
  type AutoOrganizeConfig,
  type AutoOrganizeTab,
} from './autoOrganizeModel'
import {
  AutoOrganizeBasicTab,
  AutoOrganizeNamingTab,
  AutoOrganizeScrapeTab,
} from './AutoOrganizeSettingsTabs'

type AutoOrganizeSettingsPanelProps = {
  config: AutoOrganizeConfig
  currentDir: string
  activeTab: AutoOrganizeTab
  dirty: boolean
  loading: boolean
  saving: boolean
  running: boolean
  onRefresh: () => void
  onSave: () => void
  onRunNow: () => void
  onTabChange: (tab: AutoOrganizeTab) => void
  onConfigChange: (key: keyof AutoOrganizeConfig, value: string) => void
}

const AUTO_ORGANIZE_TABS: Array<[AutoOrganizeTab, string]> = [
  ['basic', '基础设置'],
  ['naming', '命名规则'],
  ['scrape', '刮削联动'],
]

export function AutoOrganizeSettingsPanel({
  config,
  currentDir,
  activeTab,
  dirty,
  loading,
  saving,
  running,
  onRefresh,
  onSave,
  onRunNow,
  onTabChange,
  onConfigChange,
}: AutoOrganizeSettingsPanelProps) {
  return (
    <section className="glass-panel space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="font-display text-lg font-semibold text-ink-600">自动整理设置</h2>
          <p className="text-xs text-sand-500">
            设置后可自动递归扫描待整理目录，整理到媒体库目录；也可以在这里立即执行一次。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            className="neon-button"
            disabled={loading || saving}
            onClick={onRefresh}
          >
            <RefreshCw size={14} /> 重新读取
          </button>
          <button
            type="button"
            className="neon-button"
            disabled={loading || saving || !dirty}
            onClick={onSave}
          >
            {saving ? '保存中…' : '保存设置'}
          </button>
          <button
            type="button"
            className="neon-button"
            disabled={loading || saving || running}
            onClick={onRunNow}
          >
            {running ? '执行中…' : '立即整理一次'}
          </button>
        </div>
      </div>

      <div className="tab-list" role="group" aria-label="自动整理设置分类">
        {AUTO_ORGANIZE_TABS.map(([key, label]) => (
          <button
            key={key}
            type="button"
            className="tab-item"
            aria-pressed={activeTab === key}
            onClick={() => onTabChange(key)}
          >
            {label}
          </button>
        ))}
        <span className="ml-auto self-center px-2 text-xs text-sand-500">
          {dirty ? '有未保存设置' : '设置已同步'} · 定时任务名：organize_source
        </span>
      </div>

      {activeTab === 'basic' && (
        <AutoOrganizeBasicTab
          config={config}
          currentDir={currentDir}
          onConfigChange={onConfigChange}
        />
      )}
      {activeTab === 'naming' && (
        <AutoOrganizeNamingTab config={config} onConfigChange={onConfigChange} />
      )}
      {activeTab === 'scrape' && (
        <AutoOrganizeScrapeTab config={config} onConfigChange={onConfigChange} />
      )}
    </section>
  )
}
