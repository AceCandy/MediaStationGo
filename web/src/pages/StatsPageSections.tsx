import { Radio } from 'lucide-react'

import type { Hardware } from '../types'

function fmtBytes(n: number): string {
  if (!n || n <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let value = n
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(2)} ${units[unit]}`
}

export function StatsHeader({
  lastMonitorAt,
  monitorError,
}: {
  lastMonitorAt: string
  monitorError: string
}) {
  return (
    <header className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
      <div>
        <h1 className="font-display text-3xl font-bold text-ink-600">运行监控</h1>
        <p className="text-sm text-ink-50">CPU、内存与数据盘状态，每 2 秒刷新。</p>
      </div>
      <div className="glass-panel inline-flex items-center gap-3 !px-4 !py-3">
        <span className="relative flex h-3 w-3">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-70" />
          <span className="relative inline-flex h-3 w-3 rounded-full bg-emerald-500" />
        </span>
        <div>
          <p className="text-xs font-bold uppercase tracking-widest text-emerald-600">
            {monitorError ? '监控重试中' : '实时监控中'}
          </p>
          <p className="text-xs text-ink-50">
            最近刷新:{lastMonitorAt ? new Date(lastMonitorAt).toLocaleTimeString() : '—'}
          </p>
        </div>
      </div>
    </header>
  )
}

export function SystemMonitorSection({
  live,
  memPct,
  diskPct,
  monitorError,
}: {
  live: Hardware
  memPct: number
  diskPct: number
  monitorError: string
}) {
  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between gap-3">
        <h2 className="font-display text-xl font-semibold text-ink-600">系统实时指标</h2>
        <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-3 py-1 text-xs font-semibold text-emerald-700">
          <Radio size={12} />
          Live
        </span>
      </div>
      <div className="glass-panel grid gap-4 text-sm lg:grid-cols-[1fr_1.4fr]">
        <div className="grid gap-2">
          <Row label="Go 运行时" value={live.go_version} />
          <Row label="Goroutines" value={live.goroutines.toLocaleString()} />
          <Row label="内存" value={`${fmtBytes(live.memory_used)} / ${fmtBytes(live.memory_total)}`} />
          <Row label="数据盘" value={`${fmtBytes(live.disk_used)} / ${fmtBytes(live.disk_total)}`} />
        </div>
        <div className="grid gap-3">
          <Meter label="CPU" value={live.cpu_percent} />
          <Meter label="内存" value={memPct} />
          <Meter label="数据盘" value={diskPct} />
          {monitorError && <p className="text-xs text-red-500">实时监控错误:{monitorError}</p>}
        </div>
      </div>
    </section>
  )
}

function Meter({ label, value }: { label: string; value: number }) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-xs">
        <span className="font-semibold text-ink-600">{label}</span>
        <span className="font-mono text-ink-50">{value.toFixed(1)}%</span>
      </div>
      <Bar value={value} />
    </div>
  )
}

function Bar({ value }: { value: number }) {
  const pct = Math.max(0, Math.min(100, value || 0))
  const color = pct > 85 ? 'bg-red-500' : pct > 65 ? 'bg-amber-500' : 'bg-emerald-500'
  return (
    <div className="h-2 overflow-hidden rounded-full bg-gray-100">
      <div className={`h-full rounded-full transition-all duration-700 ${color}`} style={{ width: `${pct}%` }} />
    </div>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between border-b border-gray-200 pb-1 text-sm last:border-0">
      <span className="text-ink-50">{label}</span>
      <span className="font-mono text-ink-600">{value}</span>
    </div>
  )
}
