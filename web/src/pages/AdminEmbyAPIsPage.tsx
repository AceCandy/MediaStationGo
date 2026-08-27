import { ChevronDown, Code2, Copy, Globe2, Search, ShieldCheck } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import toast from 'react-hot-toast'
import { Select } from '../components/Select'
import {
  EMBY_API_CATEGORIES,
  EMBY_API_ENDPOINTS,
  type EmbyApiEndpoint,
  type EmbyApiField,
  type EmbyApiMethod,
  type EmbyApiParameterLocation,
  type EmbyApiResponse,
} from './embyApiCatalog'

const PARAMETER_LOCATIONS: readonly { id: EmbyApiParameterLocation; label: string }[] = [
  { id: 'path', label: 'Path 参数' },
  { id: 'query', label: 'Query 参数' },
  { id: 'header', label: 'Header 参数' },
  { id: 'body', label: 'Body 参数' },
]

const METHOD_CLASS: Record<EmbyApiMethod, string> = {
  GET: 'badge-sage',
  POST: 'badge-brand',
  DELETE: 'badge border border-red-400/30 bg-red-400/10 text-red-500',
  HEAD: 'badge-neutral',
}

export function AdminEmbyAPIsPage() {
  const [query, setQuery] = useState('')
  const [category, setCategory] = useState<string>('all')
  const normalizedQuery = query.trim().toLowerCase()
  const endpoints = EMBY_API_ENDPOINTS.filter((endpoint) => {
    if (category !== 'all' && endpoint.category !== category) return false
    if (!normalizedQuery) return true
    return [endpoint.name, endpoint.path, endpoint.description, ...(endpoint.aliases ?? [])]
      .some((value) => value.toLowerCase().includes(normalizedQuery))
  })
  const implementedCount = EMBY_API_ENDPOINTS.filter((endpoint) => endpoint.support === 'implemented').length

  return (
    <div className="space-y-6">
      <header>
        <div className="flex items-center gap-3">
          <Code2 className="h-7 w-7 text-brand-500" />
          <h1 className="page-heading">Emby 接口</h1>
        </div>
        <p className="page-subtitle">MediaStationGo 当前提供给 Emby 播放器的兼容接口与数据契约。</p>
      </header>

      <section className="glass-panel !p-0" aria-label="Emby 接口规则">
        <div className="grid divide-y divide-[var(--app-border)] sm:grid-cols-3 sm:divide-x sm:divide-y-0">
          <OverviewItem icon={<Code2 size={18} />} label="接口条目" value={`${EMBY_API_ENDPOINTS.length} 个`} detail={`${implementedCount} 个具备实际业务行为`} />
          <OverviewItem icon={<Globe2 size={18} />} label="路径前缀" value="/emby 或根路径" detail="大小写兼容路径已合并展示" />
          <OverviewItem icon={<ShieldCheck size={18} />} label="主要鉴权" value="X-Emby-Token" detail="兼容 Bearer、MediaBrowser 与 api_key" />
        </div>
      </section>

      <section className="grid gap-3 md:grid-cols-[minmax(0,1fr)_15rem]" aria-label="筛选接口">
        <label className="relative block">
          <span className="sr-only">搜索 Emby 接口</span>
          <Search className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-[var(--app-muted)]" />
          <input
            className="input-base !pl-11"
            type="search"
            value={query}
            placeholder="搜索名称、路径或说明"
            onChange={(event) => setQuery(event.target.value)}
          />
        </label>
        <label>
          <span className="sr-only">按接口分类筛选</span>
          <Select className="input-base" value={category} onChange={setCategory}>
            <option value="all">全部分类</option>
            {EMBY_API_CATEGORIES.map((item) => <option key={item} value={item}>{item}</option>)}
          </Select>
        </label>
      </section>

      <div className="flex items-center justify-between gap-4 text-sm text-[var(--app-muted)]">
        <p>当前显示 <span className="font-semibold text-[var(--app-text)]">{endpoints.length}</span> 个接口</p>
        {(query || category !== 'all') && (
          <button type="button" className="btn-ghost !px-2 !py-1" onClick={() => { setQuery(''); setCategory('all') }}>
            清除筛选
          </button>
        )}
      </div>

      {endpoints.length > 0 ? (
        <div className="space-y-3">
          {endpoints.map((endpoint) => <EndpointDetails key={endpoint.id} endpoint={endpoint} />)}
        </div>
      ) : (
        <div className="glass-panel py-12 text-center">
          <Search className="mx-auto h-7 w-7 text-[var(--app-muted)]" />
          <p className="mt-3 font-semibold text-[var(--app-text)]">没有匹配的接口</p>
          <p className="mt-1 text-sm text-[var(--app-muted)]">请调整搜索词或分类。</p>
        </div>
      )}
    </div>
  )
}

function OverviewItem({ icon, label, value, detail }: { icon: ReactNode; label: string; value: string; detail: string }) {
  return (
    <div className="flex min-w-0 items-start gap-3 p-5">
      <span className="mt-0.5 shrink-0 text-brand-500">{icon}</span>
      <div className="min-w-0">
        <p className="text-xs font-semibold text-[var(--app-muted)]">{label}</p>
        <p className="mt-1 break-words font-display text-base font-bold text-[var(--app-text)]">{value}</p>
        <p className="mt-1 text-xs leading-5 text-[var(--app-muted)]">{detail}</p>
      </div>
    </div>
  )
}

function EndpointDetails({ endpoint }: { endpoint: EmbyApiEndpoint }) {
  return (
    <details className="group glass-panel !p-0 [&>summary::-webkit-details-marker]:hidden">
      <summary className="flex cursor-pointer list-none items-start gap-3 p-4 sm:items-center sm:p-5">
        <div className="flex w-[4.75rem] shrink-0 flex-wrap gap-1.5">
          {endpoint.methods.map((method) => <span key={method} className={METHOD_CLASS[method]}>{method}</span>)}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="font-display text-base font-bold text-[var(--app-text)]">{endpoint.name}</h2>
            <span className={endpoint.support === 'implemented' ? 'badge-sage' : 'badge-gold !text-[var(--app-text)]'}>
              {endpoint.support === 'implemented' ? '已实现' : '兼容占位'}
            </span>
            <span className={endpoint.auth === 'token' ? 'badge-brand' : 'badge-neutral'}>
              {endpoint.auth === 'token' ? '需 Token' : '公开'}
            </span>
          </div>
          <code className="mt-2 block break-all text-xs font-semibold text-[var(--app-brand-text)] sm:text-sm">{endpoint.path}</code>
          <p className="mt-2 text-sm leading-6 text-[var(--app-muted)]">{endpoint.description}</p>
        </div>
        <ChevronDown className="mt-1 h-5 w-5 shrink-0 text-[var(--app-muted)] transition-transform group-open:rotate-180 sm:mt-0" />
      </summary>

      <div className="border-t border-[var(--app-border)] px-4 pb-5 pt-4 sm:px-5">
        <div className="space-y-6">
          <PathSection endpoint={endpoint} />
          <ParametersSection endpoint={endpoint} />
          {endpoint.requestExample && <CodeBlock title="请求示例" value={endpoint.requestExample} />}
          <ResponsesSection responses={endpoint.responses} />
          {endpoint.notes && endpoint.notes.length > 0 && (
            <section>
              <h3 className="text-xs font-bold uppercase text-[var(--app-muted)]">补充说明</h3>
              <ul className="mt-2 space-y-1.5 text-sm leading-6 text-[var(--app-subtle)]">
                {endpoint.notes.map((note) => <li key={note}>• {note}</li>)}
              </ul>
            </section>
          )}
        </div>
      </div>
    </details>
  )
}

function PathSection({ endpoint }: { endpoint: EmbyApiEndpoint }) {
  const paths = [endpoint.path, ...(endpoint.aliases ?? [])]
  return (
    <section>
      <h3 className="text-xs font-bold uppercase text-[var(--app-muted)]">路径与别名</h3>
      <div className="mt-2 space-y-2">
        {paths.map((path, index) => (
          <div key={path} className="flex min-w-0 items-center gap-2 border-b border-[var(--app-border)] py-2 last:border-b-0">
            <span className="w-10 shrink-0 text-xs text-[var(--app-muted)]">{index === 0 ? '主路径' : '别名'}</span>
            <code className="min-w-0 flex-1 break-all text-xs text-[var(--app-text)] sm:text-sm">{path}</code>
            <CopyButton value={path} label={`复制路径 ${path}`} />
          </div>
        ))}
      </div>
    </section>
  )
}

function ParametersSection({ endpoint }: { endpoint: EmbyApiEndpoint }) {
  if (endpoint.parameters.length === 0) {
    return (
      <section>
        <h3 className="text-xs font-bold uppercase text-[var(--app-muted)]">请求参数</h3>
        <p className="mt-2 text-sm text-[var(--app-muted)]">无参数</p>
      </section>
    )
  }

  return (
    <section>
      <h3 className="text-xs font-bold uppercase text-[var(--app-muted)]">请求参数</h3>
      <div className="mt-3 space-y-5">
        {PARAMETER_LOCATIONS.map(({ id, label }) => {
          const parameters = endpoint.parameters.filter((parameter) => parameter.location === id)
          if (parameters.length === 0) return null
          return (
            <div key={id}>
              <h4 className="text-sm font-semibold text-[var(--app-text)]">{label}</h4>
              <div className="mt-2 divide-y divide-[var(--app-border)] border-y border-[var(--app-border)]">
                {parameters.map((parameter) => (
                  <div key={`${id}-${parameter.name}`} className="grid gap-1 py-3 text-sm md:grid-cols-[minmax(8rem,0.8fr)_7rem_minmax(0,2fr)] md:gap-4">
                    <div className="min-w-0">
                      <code className="break-all font-semibold text-[var(--app-brand-text)]">{parameter.name}</code>
                      {parameter.required && <span className="ml-2 text-xs font-semibold text-red-500">必填</span>}
                    </div>
                    <span className="text-xs text-[var(--app-muted)] md:pt-0.5">{parameter.type}</span>
                    <p className="leading-6 text-[var(--app-subtle)]">{parameter.description}</p>
                  </div>
                ))}
              </div>
            </div>
          )
        })}
      </div>
    </section>
  )
}

function ResponsesSection({ responses }: { responses: readonly EmbyApiResponse[] }) {
  return (
    <section>
      <h3 className="text-xs font-bold uppercase text-[var(--app-muted)]">响应</h3>
      <div className="mt-3 divide-y divide-[var(--app-border)] border-y border-[var(--app-border)]">
        {responses.map((response) => (
          <div key={`${response.status}-${response.description}`} className="space-y-3 py-4">
            <div className="flex flex-wrap items-center gap-2">
              <span className="badge-brand">{response.status}</span>
              <code className="break-all text-xs text-[var(--app-muted)]">{response.contentType}</code>
            </div>
            <p className="text-sm leading-6 text-[var(--app-subtle)]">{response.description}</p>
            {response.fields && <FieldsList fields={response.fields} />}
            {response.example && <CodeBlock title="响应示例" value={response.example} />}
          </div>
        ))}
      </div>
    </section>
  )
}

function FieldsList({ fields }: { fields: readonly EmbyApiField[] }) {
  return (
    <div className="divide-y divide-[var(--app-border)]">
      {fields.map((field) => (
        <div key={field.name} className="grid gap-1 py-2 text-sm md:grid-cols-[minmax(9rem,0.9fr)_7rem_minmax(0,2fr)] md:gap-4">
          <code className="break-all font-semibold text-[var(--app-text)]">{field.name}</code>
          <span className="text-xs text-[var(--app-muted)] md:pt-0.5">{field.type}</span>
          <p className="leading-6 text-[var(--app-subtle)]">{field.description}</p>
        </div>
      ))}
    </div>
  )
}

function CodeBlock({ title, value }: { title: string; value: string }) {
  return (
    <div className="overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--app-panel-soft)]">
      <div className="flex items-center justify-between border-b border-[var(--app-border)] px-3 py-2">
        <span className="text-xs font-semibold text-[var(--app-muted)]">{title}</span>
        <CopyButton value={value} label={`复制${title}`} />
      </div>
      <pre className="max-w-full overflow-x-auto p-3 text-xs leading-6 text-[var(--app-subtle)]"><code>{value}</code></pre>
    </div>
  )
}

function CopyButton({ value, label }: { value: string; label: string }) {
  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      toast.success('已复制')
    } catch {
      toast.error('复制失败')
    }
  }

  return (
    <button type="button" className="icon-btn !h-8 !w-8" title={label} aria-label={label} onClick={() => void copy()}>
      <Copy size={15} />
    </button>
  )
}
