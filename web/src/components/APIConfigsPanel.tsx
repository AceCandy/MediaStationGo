import { FormEvent, useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { ChevronDown, ChevronUp, KeyRound, Pencil, Plus, Save, Trash2, X } from 'lucide-react'

import {
  apiConfigsAPI,
  type APIConfig,
  type APIConfigPatch,
  type ProxyPoolInput,
  type ProxyPoolItem,
} from '../api/api_configs'
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

      <ProxyPoolPanel />
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
  const [useProxyPool, setUseProxyPool] = useState(item.use_proxy_pool ?? false)
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
      if (isDouban) {
        patch.image_direct = imageDirect
        patch.use_proxy_pool = useProxyPool
      }
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
                <>
                  <label className="flex items-center gap-2 text-xs text-ink-50 md:col-span-2">
                    <input
                      type="checkbox"
                      checked={useProxyPool}
                      onChange={(e) => setUseProxyPool(e.target.checked)}
                    />
                    使用代理池（仅 HTTP 400 时切换）
                  </label>
                  <label className="flex items-center gap-2 text-xs text-ink-50 md:col-span-2">
                    <input
                      type="checkbox"
                      checked={imageDirect}
                      onChange={(e) => setImageDirect(e.target.checked)}
                    />
                    图片直连（失败后使用 curl）
                  </label>
                </>
              )}
            </div>
          </details>
        </form>
      </td>
    </tr>
  )
}

type EditableProxy = ProxyPoolItem & { url: string }

function ProxyPoolPanel() {
  const [items, setItems] = useState<EditableProxy[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [loadError, setLoadError] = useState('')

  const refresh = async () => {
    setLoading(true)
    setLoadError('')
    try {
      const loaded = await apiConfigsAPI.listProxyPool()
      setItems(loaded.map((item) => ({ ...item, url: '' })))
    } catch (err: unknown) {
      setLoadError(apiErrorMessage(err, '代理池加载失败'))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void refresh()
  }, [])

  const save = async () => {
    setSaving(true)
    try {
      const input: ProxyPoolInput[] = items.map((item) => ({
        ...(item.id ? { id: item.id } : {}),
        ...(item.url.trim() ? { url: item.url.trim() } : {}),
      }))
      const saved = await apiConfigsAPI.replaceProxyPool(input)
      setItems(saved.map((item) => ({ ...item, url: '' })))
      toast.success('代理池已保存')
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, '代理池保存失败'))
    } finally {
      setSaving(false)
    }
  }

  const move = (index: number, offset: number) => {
    const target = index + offset
    if (target < 0 || target >= items.length) return
    setItems((current) => {
      const next = [...current]
      const moved = next[index]
      next[index] = next[target]
      next[target] = moved
      return next
    })
  }

  return (
    <div className="glass-panel p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="font-medium text-ink-600">代理池</p>
          <p className="text-xs text-sand-500">按列表顺序尝试；认证信息加密保存且不会回显。</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setItems((current) => [...current, { id: '', display_url: '', has_auth: false, url: '' }])}
            disabled={loading || saving || Boolean(loadError)}
            className="rounded-lg border border-sand-400/30 px-3 py-1.5 text-xs text-ink-50 hover:text-white disabled:opacity-50"
          >
            <Plus className="mr-1 inline" size={12} /> 新增
          </button>
          <button
            type="button"
            onClick={() => void save()}
            disabled={loading || saving || Boolean(loadError) || items.some((item) => !item.id && !item.url.trim())}
            className="neon-button !px-3 !py-1.5 !text-xs"
          >
            <Save size={12} /> 保存
          </button>
        </div>
      </div>

      {loading && <p className="py-5 text-center text-sm text-sand-500">加载中…</p>}
      {!loading && loadError && (
        <div className="mt-3 flex items-center justify-between gap-3 text-sm text-red-700">
          <span className="break-words">{loadError}</span>
          <button type="button" onClick={() => void refresh()} className="shrink-0 font-medium hover:text-red-900">
            重试
          </button>
        </div>
      )}
      {!loading && !loadError && items.length === 0 && (
        <p className="py-5 text-center text-sm text-sand-500">暂无代理节点</p>
      )}
      {!loading && !loadError && items.length > 0 && (
        <div className="mt-3 space-y-2">
          {items.map((item, index) => (
            <div key={item.id || `new-${index}`} className="grid gap-2 border border-gray-200 p-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] md:items-center">
              <div className="min-w-0 text-xs">
                <p className="truncate font-mono text-ink-600">{item.display_url || `新节点 ${index + 1}`}</p>
                {item.has_auth && <p className="mt-1 text-brand-500">已配置认证</p>}
              </div>
              <input
                type="password"
                aria-label={item.id ? `替换代理 ${index + 1}` : `新增代理 ${index + 1}`}
                className="input-base"
                placeholder={item.id ? '输入新地址以替换；留空保留' : 'http://user:pass@host:port'}
                value={item.url}
                onChange={(event) => setItems((current) => current.map((row, rowIndex) => (
                  rowIndex === index ? { ...row, url: event.target.value } : row
                )))}
              />
              <div className="flex items-center justify-end gap-1">
                <button type="button" aria-label={`上移代理 ${index + 1}`} onClick={() => move(index, -1)} disabled={index === 0 || saving} className="rounded-lg p-1.5 text-ink-50 hover:bg-gray-50 hover:text-white disabled:opacity-30" title="上移">
                  <ChevronUp size={14} />
                </button>
                <button type="button" aria-label={`下移代理 ${index + 1}`} onClick={() => move(index, 1)} disabled={index === items.length - 1 || saving} className="rounded-lg p-1.5 text-ink-50 hover:bg-gray-50 hover:text-white disabled:opacity-30" title="下移">
                  <ChevronDown size={14} />
                </button>
                <button type="button" aria-label={`删除代理 ${index + 1}`} onClick={() => setItems((current) => current.filter((_, rowIndex) => rowIndex !== index))} disabled={saving} className="rounded-lg p-1.5 text-ink-50 hover:bg-red-400/10 hover:text-red-400 disabled:opacity-30" title="删除">
                  <Trash2 size={14} />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function apiErrorMessage(err: unknown, fallback: string): string {
  return (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? fallback
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
