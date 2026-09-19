import { FormEvent, useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { Activity, KeyRound, Pencil, Save, Trash2, X } from 'lucide-react'

import {
  apiConfigsAPI,
  type APIConfig,
  type APIConfigPatch,
  type ProxyPoolConfig,
  type ProxyPoolInput,
  type ProxyPoolItem,
} from '../api/api_configs'
import { Select } from './Select'
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
        <div className="glass-panel overflow-x-auto">
          <table className="data-table">
            <thead>
              <tr>
                <th>服务</th>
                <th>密钥 / 状态</th>
                <th className="text-right">操作</th>
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
                  <tr key={item.id}>
                    <td>
                      <p className="font-medium text-ink-600">{item.provider}</p>
                      {item.description && (
                        <p className="text-xs text-sand-500">{item.description}</p>
                      )}
                    </td>
                    <td>
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
                    <td className="text-right">
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
    <tr className="bg-primary-400/5">
      <td colSpan={3}>
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
          <section className="border-t border-gray-200 pt-3">
            <h3 className="text-xs font-semibold text-ink-600">高级设置</h3>
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
                    使用代理（HTTP 400 或 unexpected EOF 时切换）
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
          </section>
        </form>
      </td>
    </tr>
  )
}

function ProxyPoolPanel() {
  const [items, setItems] = useState<ProxyPoolItem[]>([])
  const [value, setValue] = useState('')
  const [config, setConfig] = useState<ProxyPoolConfig>({
    proxy_pool_type: 'normal',
    has_resin_proxy_token: false,
  })
  const [resinProxyURL, setResinProxyURL] = useState('')
  const [resinProxyToken, setResinProxyToken] = useState('')
  const [resinAccount, setResinAccount] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [checking, setChecking] = useState(false)
  const [loadError, setLoadError] = useState('')

  const refresh = async () => {
    setLoading(true)
    setLoadError('')
    try {
      const [loaded, loadedConfig] = await Promise.all([
        apiConfigsAPI.listProxyPool(),
        apiConfigsAPI.getProxyPoolConfig(),
      ])
      setItems(loaded)
      setValue(proxyPoolText(loaded))
      setConfig(loadedConfig)
      setResinProxyURL(loadedConfig.resin_proxy_url ?? '')
      setResinAccount(loadedConfig.resin_account ?? '')
      setResinProxyToken('')
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
      const patch = {
        proxy_pool_type: config.proxy_pool_type,
        resin_proxy_url: resinProxyURL,
        resin_account: resinAccount,
        ...(resinProxyToken.trim() ? { resin_proxy_token: resinProxyToken.trim() } : {}),
      }
      const savedConfig = await apiConfigsAPI.updateProxyPoolConfig(patch)
      setConfig(savedConfig)
      setResinProxyURL(savedConfig.resin_proxy_url ?? '')
      setResinAccount(savedConfig.resin_account ?? '')
      setResinProxyToken('')
      if (config.proxy_pool_type === 'normal') {
        const retained = new Set<number>()
        const input: ProxyPoolInput[] = value
          .split(/\r?\n/)
          .map((line) => line.trim())
          .filter((line) => line && line !== '!')
          .map((line) => {
            const replace = line.startsWith('!')
            const url = replace ? line.slice(1).trim() : line
            if (replace) return { url }
            const index = items.findIndex((item, itemIndex) => (
              !retained.has(itemIndex) && item.display_url === url
            ))
            if (index < 0) return { url }
            retained.add(index)
            return { id: items[index].id }
          })
        const saved = await apiConfigsAPI.replaceProxyPool(input)
        setItems(saved)
        setValue(proxyPoolText(saved))
      }
      toast.success('代理池已保存')
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, '代理池保存失败'))
    } finally {
      setSaving(false)
    }
  }

  const checkAndCleanup = async () => {
    setChecking(true)
    try {
      const result = await apiConfigsAPI.checkProxyPool()
      const summary = `共 ${result.total} 个：可用 ${result.available}，不可用 ${result.unavailable}，无法判定 ${result.inconclusive}`
      if (result.unavailable === 0) {
        toast.success(`检测完成，${summary}`)
        return
      }
      const dirtyWarning = value === proxyPoolText(items)
        ? ''
        : ' 当前文本框有未保存修改，清理成功后将以服务端结果覆盖。'
      const confirmed = await confirmAction({
        title: '清理不可用代理',
        message: `检测完成，${summary}。将永久删除 ${result.unavailable} 个确定不可用代理，此操作不可恢复。${dirtyWarning}`,
        confirmText: '确认清理',
      })
      if (!confirmed) return
      if (!result.cleanup_token) throw new Error('missing proxy cleanup token')
      const cleaned = await apiConfigsAPI.cleanupProxyPool(result.cleanup_token)
      setItems(cleaned.items)
      setValue(proxyPoolText(cleaned.items))
      toast.success(`已剔除 ${cleaned.removed} 个不可用代理`)
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, '代理池检测失败'))
    } finally {
      setChecking(false)
    }
  }

  return (
    <div className="glass-panel p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="font-medium text-ink-600">代理池</p>
          <p className="text-xs text-sand-500">
            {config.proxy_pool_type === 'normal'
              ? '每行一个代理，行顺序就是尝试顺序；空行会被忽略。'
              : 'Resin 通过 URL 反向代理统一调度内部节点。'}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {config.proxy_pool_type === 'normal' && (
            <button
              type="button"
              onClick={() => void checkAndCleanup()}
              disabled={loading || saving || checking || Boolean(loadError) || items.length === 0}
              className="btn-outline !px-3 !py-1.5 !text-xs"
            >
              <Activity size={12} /> {checking ? '检测中…' : '检测不可用代理'}
            </button>
          )}
          <button
            type="button"
            onClick={() => void save()}
            disabled={loading || saving || checking || Boolean(loadError) || (
              config.proxy_pool_type === 'resin'
              && (!resinProxyURL.trim() || (!config.has_resin_proxy_token && !resinProxyToken.trim()))
            )}
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
      {!loading && !loadError && (
        <div className="mt-3 space-y-3">
          <label className="block text-xs text-ink-50">
            代理类型
            <Select
              className="input-base mt-1 w-full"
              value={config.proxy_pool_type}
              onChange={(proxyPoolType) => setConfig({ ...config, proxy_pool_type: proxyPoolType as 'normal' | 'resin' })}
            >
              <option value="normal">普通代理池</option>
              <option value="resin">Resin 代理池</option>
            </Select>
          </label>
          {config.proxy_pool_type === 'normal' ? (
            <label className="block text-xs text-ink-50">
              代理地址
              <textarea
                className="input-base mt-1 min-h-40 resize-y font-mono text-xs"
                placeholder={'http://user:pass@proxy.example:8080\nsocks5://proxy.example:1080'}
                value={value}
                onChange={(event) => setValue(event.target.value)}
                disabled={checking}
                autoComplete="off"
                spellCheck={false}
              />
              <span className="mt-1 block text-sand-500">
                已保存 {items.length} 个节点，其中 {items.filter((item) => item.has_auth).length} 个含认证；未改动的脱敏行会保留原认证。
                需要强制替换或移除认证时，在该行开头加 !。
              </span>
            </label>
          ) : (
            <div className="grid gap-3 md:grid-cols-2">
              <label className="text-xs text-ink-50">
                Resin 实例地址
                <input
                  className="input-base mt-1"
                  type="url"
                  placeholder="https://resin.example.com"
                  value={resinProxyURL}
                  onChange={(event) => setResinProxyURL(event.target.value)}
                />
              </label>
              <label className="text-xs text-ink-50">
                RESIN_PROXY_TOKEN
                <input
                  className="input-base mt-1"
                  type="password"
                  placeholder={config.has_resin_proxy_token ? '•••••••••••• (留空保留原值)' : '输入代理 Token'}
                  value={resinProxyToken}
                  onChange={(event) => setResinProxyToken(event.target.value)}
                />
              </label>
              <label className="text-xs text-ink-50 md:col-span-2">
                粘性标识（可选）
                <input
                  className="input-base mt-1"
                  maxLength={128}
                  placeholder="留空时由 Resin 随机调度"
                  value={resinAccount}
                  onChange={(event) => setResinAccount(event.target.value)}
                />
              </label>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function proxyPoolText(items: ProxyPoolItem[]): string {
  return items.map((item) => item.display_url).join('\n')
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
