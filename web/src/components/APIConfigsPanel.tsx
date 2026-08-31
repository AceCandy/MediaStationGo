import { FormEvent, useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { KeyRound, Pencil, Save, Trash2, X } from 'lucide-react'

import { apiConfigsAPI, type APIConfig, type APIConfigPatch } from '../api/api_configs'
import { confirmAction } from './confirmAction'

// Compact inline-editable provider table for the External API management page.
export function APIConfigsPanel() {
  const [items, setItems] = useState<APIConfig[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [editing, setEditing] = useState<string | null>(null)

  const refresh = async () => {
    setLoading(true)
    setLoadError('')
    try {
      setItems(await apiConfigsAPI.list())
    } catch (err: unknown) {
      const message =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '外部 API 配置加载失败'
      setItems([])
      setLoadError(message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void refresh()
  }, [])

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <KeyRound className="h-5 w-5 text-brand-500" />
        <div>
          <p className="font-display text-lg font-semibold text-ink-600">外部 API 配置</p>
          <p className="text-xs text-ink-50">
            TMDb / Bangumi / TheTVDB / Fanart / OpenAI / Douban / Adult 密钥与源管理
          </p>
        </div>
      </div>

      {loading && (
        <p className="py-6 text-center text-sm text-sand-500">加载中…</p>
      )}

      {!loading && loadError && (
        <div className="flex items-center justify-between gap-3 border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          <span className="break-words">{loadError}</span>
          <button
            type="button"
            onClick={() => void refresh()}
            className="shrink-0 text-sm font-medium text-red-700 hover:text-red-900"
          >
            重试
          </button>
        </div>
      )}

      {!loading && !loadError && (
        <div className="glass-panel overflow-hidden">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-gray-200 text-xs uppercase tracking-wider text-sand-500">
              <tr>
                <th className="px-4 py-3">服务</th>
                <th className="px-4 py-3">密钥 / 状态</th>
                <th className="px-4 py-3 text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) =>
                editing === item.provider ? (
                  <EditingRow
                    key={item.id}
                    item={item}
                    onCancel={() => setEditing(null)}
                    onSaved={() => {
                      setEditing(null)
                      refresh()
                    }}
                  />
                ) : (
                  <tr
                    key={item.id}
                    className="border-t border-gray-200 transition hover:bg-gray-50"
                  >
                    <td className="px-4 py-3">
                      <p className="font-medium text-ink-600">{item.provider}</p>
                      {item.description && (
                        <p className="text-xs text-sand-500">{item.description}</p>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex flex-wrap items-center gap-2 text-xs">
                        {apiConfigConfigured(item) && item.masked_key && (
                          <span className="font-mono text-brand-500">{item.masked_key}</span>
                        )}
                        {item.provider === 'adult' && apiConfigSourceCount(item) > 0 && (
                          <span className="font-mono text-brand-500">{apiConfigSourceCount(item)} 个源</span>
                        )}
                        {apiConfigConfigured(item) ? (
                          <span className="inline-flex rounded-full bg-emerald-400/10 px-2 py-0.5 text-emerald-400">
                            已配置
                          </span>
                        ) : (
                          <span className="inline-flex rounded-full bg-sand-300/40 px-2 py-0.5 text-sand-500">
                            未配置
                          </span>
                        )}
                      </div>
                    </td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <button
                          onClick={() => setEditing(item.provider)}
                          className="rounded-lg p-1.5 text-ink-50 transition hover:bg-gray-50 hover:text-white"
                          title="编辑"
                        >
                          <Pencil size={14} />
                        </button>
                        {item.has_key && (
                          <button
                            onClick={async () => {
                              if (!(await confirmAction({
                                title: item.provider === 'douban' ? '清除 Cookie' : '清除 API Key',
                                message: item.provider === 'douban'
                                  ? `确定清除 ${item.provider} 的 Cookie?`
                                  : `确定清除 ${item.provider} 的 API Key?`,
                                confirmText: '清除',
                              }))) return
                              await apiConfigsAPI.remove(item.provider)
                              toast.success('已清除')
                              refresh()
                            }}
                            className="rounded-lg p-1.5 text-ink-50 transition hover:bg-red-400/10 hover:text-red-400"
                            title={item.provider === 'douban' ? '清除 Cookie' : '清除密钥'}
                          >
                            <Trash2 size={14} />
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ),
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function EditingRow({
  item,
  onCancel,
  onSaved,
}: {
  item: APIConfig
  onCancel: () => void
  onSaved: () => void
}) {
  const [apiKey, setAPIKey] = useState('')
  const [baseURL, setBaseURL] = useState(item.base_url ?? '')
  const [model, setModel] = useState(item.model ?? '')
  const [extra, setExtra] = useState(item.extra ?? '')
  const [enabled, setEnabled] = useState(item.enabled)
  const [imageDirect, setImageDirect] = useState(item.image_direct ?? false)
  const [webSearchEnabled, setWebSearchEnabled] = useState(item.web_search_enabled)
  const [saving, setSaving] = useState(false)
  const isAdult = item.provider === 'adult'
  const isDouban = item.provider === 'douban'
  const isOpenAI = item.provider === 'openai'

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    try {
      const patch: APIConfigPatch = { base_url: baseURL, enabled }
      if (isAdult) patch.extra = extra
      if (isDouban) patch.image_direct = imageDirect
      if (isOpenAI) {
        patch.model = model
        patch.web_search_enabled = webSearchEnabled
      }
      if (apiKey.trim()) patch.api_key = apiKey.trim()
      await apiConfigsAPI.update(item.provider, patch)
      toast.success(`${item.provider} 已保存`)
      onSaved()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '保存失败'
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }

  return (
    <tr className="border-t border-gray-200 bg-primary-400/5">
      <td colSpan={3} className="px-4 py-3">
        <form onSubmit={submit} className="space-y-3">
          <div className="flex flex-wrap items-end gap-3">
            <span className="text-sm font-medium text-ink-600">{item.provider}</span>
            {!isAdult && (
              <label className="min-w-64 flex-1 text-xs text-ink-50">
                {isDouban ? 'Cookie' : 'API Key'}
                <input
                  className="input-base mt-1"
                  type="password"
                  placeholder={item.has_key ? '•••••••••••• (留空保留原值)' : isDouban ? '输入 Cookie' : '输入密钥'}
                  value={apiKey}
                  onChange={(e) => setAPIKey(e.target.value)}
                />
              </label>
            )}
            <label className="flex items-center gap-2 text-xs text-ink-50">
              <input
                type="checkbox"
                checked={enabled}
                onChange={(e) => setEnabled(e.target.checked)}
              />
              启用
            </label>
            <button type="submit" disabled={saving} className="neon-button !px-3 !py-1.5 !text-xs">
              <Save size={12} /> 保存
            </button>
            <button
              type="button"
              onClick={onCancel}
              className="rounded-lg border border-sand-400/30 px-2 py-1.5 text-xs text-ink-50 hover:text-white"
              title="取消编辑"
            >
              <X size={12} />
            </button>
          </div>
          <details className="border-t border-gray-200 pt-3">
            <summary className="cursor-pointer text-xs font-semibold text-ink-600">高级设置</summary>
            <div className="mt-3 grid gap-3 md:grid-cols-2">
              <label className="text-xs text-ink-50">
                {isAdult ? '主源 URL' : 'Base URL'}
                <input
                  className="input-base mt-1"
                  placeholder={isAdult ? 'https://javdb.com' : 'https://api.themoviedb.org/3'}
                  value={baseURL}
                  onChange={(e) => setBaseURL(e.target.value)}
                />
              </label>
              {isOpenAI && (
                <label className="text-xs text-ink-50">
                  模型
                  <input
                    className="input-base mt-1"
                    placeholder="gpt-4o-mini"
                    value={model}
                    onChange={(e) => setModel(e.target.value)}
                  />
                </label>
              )}
              {isAdult && (
                <label className="text-xs text-ink-50 md:col-span-2">
                  备用源 URL
                  <textarea
                    className="input-base mt-1 min-h-20 resize-y"
                    placeholder={'https://javbus.sbs\nhttps://www.javbus.com'}
                    value={extra}
                    onChange={(e) => setExtra(e.target.value)}
                  />
                </label>
              )}
              {isOpenAI && (
                <label className="flex items-center gap-2 text-xs text-ink-50 md:col-span-2">
                  <input
                    type="checkbox"
                    checked={webSearchEnabled}
                    onChange={(e) => setWebSearchEnabled(e.target.checked)}
                  />
                  联网搜索
                </label>
              )}
              {isDouban && (
                <label className="flex items-center gap-2 text-xs text-ink-50 md:col-span-2">
                  <input
                    type="checkbox"
                    checked={imageDirect}
                    onChange={(e) => setImageDirect(e.target.checked)}
                  />
                  图片直连（失败后使用 curl）
                </label>
              )}
            </div>
          </details>
        </form>
      </td>
    </tr>
  )
}

function apiConfigConfigured(item: APIConfig): boolean {
  if (item.provider === 'adult') {
    return Boolean(item.base_url?.trim() || item.extra?.trim())
  }
  return item.has_key
}

function apiConfigSourceCount(item: APIConfig): number {
  if (item.provider !== 'adult') return 0
  return [item.base_url, item.extra]
    .join('\n')
    .split(/[\s,;]+/)
    .map((value) => value.trim())
    .filter(Boolean).length
}
