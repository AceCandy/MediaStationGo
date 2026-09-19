import { useEffect, useMemo, useState, type ReactNode } from 'react'
import toast from 'react-hot-toast'
import {
  CheckCircle2,
  Copy,
  Files,
  HardDrive,
  Layers,
  ScanSearch,
  Tags,
  Trash2,
} from 'lucide-react'

import {
  duplicatesAPI,
  type DuplicateGroup,
  type DuplicateMedia,
  type DuplicateReport,
} from '../api/duplicates'
import { libraryAPI } from '../api/library'
import { confirmAction } from '../components/confirmAction'
import { Select } from '../components/Select'
import type { Library } from '../types'

function fmtBytes(n: number): string {
  if (!n) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(1)} ${u[i]}`
}

const TONE_STYLES = {
  brand: 'border-brand-200/60 bg-brand-50 text-brand-500',
  sage: 'border-sage-200/60 bg-sage-50 text-sage-600',
  amber: 'border-amber-300/60 bg-amber-50 text-amber-600',
  red: 'border-red-300/60 bg-red-50 text-red-500',
} as const

function StatCard({
  icon,
  label,
  value,
  tone,
}: {
  icon: ReactNode
  label: string
  value: string
  tone: keyof typeof TONE_STYLES
}) {
  return (
    <div className="glass-panel flex items-center gap-3 !p-4">
      <div
        className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border ${TONE_STYLES[tone]}`}
      >
        {icon}
      </div>
      <div className="min-w-0">
        <p className="text-2xs font-bold uppercase tracking-widest text-ink-50">{label}</p>
        <p className="truncate font-display text-xl font-bold text-ink-600">{value}</p>
      </div>
    </div>
  )
}

function MediaRow({ media }: { media: DuplicateMedia }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium text-ink-600" title={media.title}>
          {media.title}
        </p>
        <p className="mt-0.5 truncate font-mono text-2xs text-ink-50" title={media.path}>
          {media.path}
        </p>
      </div>
      <div className="shrink-0 text-right text-xs text-ink-50">
        <p className="font-semibold text-ink-100">{fmtBytes(media.size_bytes)}</p>
        {media.library_name ? <p className="mt-0.5">{media.library_name}</p> : null}
      </div>
    </div>
  )
}

function DuplicateGroupCard({ group, index }: { group: DuplicateGroup; index: number }) {
  const releasable = group.duplicates.reduce((sum, d) => sum + d.size_bytes, 0)
  return (
    <section className="glass-panel space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
        <div className="flex items-center gap-2">
          <span className="badge-brand">重复组 #{index + 1}</span>
          <code
            className="rounded-md bg-gray-100 px-2 py-0.5 font-mono text-2xs text-ink-50"
            title={group.hash}
          >
            {group.hash.length > 16 ? `${group.hash.slice(0, 16)}…` : group.hash}
          </code>
        </div>
        <p className="text-xs text-ink-50">
          {group.duplicates.length + 1} 个相同文件 · 可释放{' '}
          <span className="font-semibold text-red-400">{fmtBytes(releasable)}</span>
        </p>
      </div>

      <div className="rounded-xl border border-emerald-300/50 bg-emerald-50/50 p-3">
        <p className="mb-1.5 flex items-center gap-1.5 text-xs font-semibold text-emerald-600">
          <CheckCircle2 size={14} /> 保留主条目
        </p>
        <MediaRow media={group.primary} />
      </div>

      <div className="space-y-1.5">
        <p className="flex items-center gap-1.5 text-xs font-semibold text-red-400">
          <Copy size={13} /> 重复项（{group.duplicates.length}）
        </p>
        {group.duplicates.map((d) => (
          <div
            key={d.id}
            className="rounded-xl border border-gray-200/80 bg-gray-50/50 p-3 transition hover:border-red-300/60"
          >
            <MediaRow media={d} />
          </div>
        ))}
      </div>
    </section>
  )
}

export function DuplicatesPage() {
  const [libs, setLibs] = useState<Library[]>([])
  const [libID, setLibID] = useState('')
  const [report, setReport] = useState<DuplicateReport | null>(null)
  const [scanning, setScanning] = useState(false)

  useEffect(() => {
    libraryAPI.list().then(setLibs)
  }, [])

  useEffect(() => {
    duplicatesAPI.list(libID).then(setReport).catch(() => setReport(null))
  }, [libID])

  const releasableTotal = useMemo(
    () =>
      (report?.groups ?? []).reduce(
        (sum, g) => sum + g.duplicates.reduce((s, d) => s + d.size_bytes, 0),
        0,
      ),
    [report],
  )

  const scan = async () => {
    setScanning(true)
    try {
      const r = await duplicatesAPI.scan(libID)
      setReport(r)
      const cleaned = r.missing_removed ? `, 清理 ${r.missing_removed} 条失效记录` : ''
      toast.success(`扫描完成: ${r.groups_found} 组重复, ${r.items_marked} 项标记${cleaned}`)
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '扫描失败'
      toast.error(msg)
    } finally {
      setScanning(false)
    }
  }

  const unmark = async () => {
    if (!(await confirmAction({ title: '清除重复标记', message: '清除所有重复标记?(磁盘文件不会被删除)', confirmText: '清除' }))) return
    const r = await duplicatesAPI.unmark(libID)
    toast.success(`已清除 ${r.unmarked} 项`)
    setReport(null)
  }

  const groups = report?.groups ?? []

  return (
    <div className="space-y-6">
      <p className="text-sm text-ink-50">扫描媒体库中的重复文件，并标记重复条目；不会删除磁盘文件。</p>

      <div className="glass-panel flex flex-col gap-3 md:flex-row md:items-end">
        <div className="min-w-0 flex-1">
          <label className="input-label">媒体库</label>
          <Select
            className="input-base"
            value={libID}
            onChange={setLibID}
          >
            <option value="">所有媒体库</option>
            {libs.map((l) => (
              <option key={l.id} value={l.id}>
                {l.name}
              </option>
            ))}
          </Select>
        </div>
        <div className="flex gap-2">
          <button onClick={scan} disabled={scanning} className="btn-primary flex-1 md:flex-none">
            <ScanSearch size={15} />
            {scanning ? '扫描中…' : '开始扫描'}
          </button>
          <button
            onClick={unmark}
            className="btn-outline flex-1 !border-red-300/60 !text-red-500 hover:!border-red-400 hover:!text-red-500 md:flex-none"
          >
            <Trash2 size={14} /> 清除标记
          </button>
        </div>
      </div>

      {report && (
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <StatCard
            icon={<Files size={18} />}
            label="扫描条目"
            value={String(report.total_scanned)}
            tone="brand"
          />
          <StatCard
            icon={<Layers size={18} />}
            label="重复组"
            value={String(report.groups_found)}
            tone="sage"
          />
          <StatCard
            icon={<Tags size={18} />}
            label="标记项"
            value={String(report.items_marked)}
            tone="amber"
          />
          <StatCard
            icon={<HardDrive size={18} />}
            label="可释放空间"
            value={fmtBytes(releasableTotal)}
            tone="red"
          />
        </div>
      )}

      {report && report.missing_removed ? (
        <p className="rounded-2xl border border-amber-300/50 bg-amber-50 px-4 py-3 text-sm text-amber-700">
          已清理 {report.missing_removed} 条文件不存在的媒体记录，统计容量会在刷新后恢复正常。
        </p>
      ) : null}

      {report && groups.length === 0 && (
        <div className="glass-panel flex flex-col items-center gap-2 py-12 text-center">
          <div className="modal-icon">
            <CheckCircle2 size={20} />
          </div>
          <p className="font-medium text-ink-600">未发现重复文件</p>
          <p className="text-sm text-ink-50">已扫描 {report.total_scanned} 项媒体。</p>
        </div>
      )}

      {groups.map((g, i) => (
        <DuplicateGroupCard key={g.hash} group={g} index={i} />
      ))}
    </div>
  )
}
