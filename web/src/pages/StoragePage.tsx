import { useEffect, useState } from 'react'
import { HardDrive, Library } from 'lucide-react'

import { storageAPI, type StorageBreakdown } from '../api/storage'
import { statsAPI } from '../api/stats'
import type { Hardware } from '../types'
import { StatsHeader, SystemMonitorSection } from './StatsPageSections'

function fmtBytes(n: number): string {
  if (!n) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let v = n
  let i = 0
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(2)} ${u[i]}`
}

// StoragePage combines storage breakdowns with live host metrics.
export function StoragePage() {
  const [data, setData] = useState<StorageBreakdown | null>(null)
  const [storageLoading, setStorageLoading] = useState(true)
  const [storageError, setStorageError] = useState('')
  const [hardware, setHardware] = useState<Hardware | null>(null)
  const [lastMonitorAt, setLastMonitorAt] = useState('')
  const [monitorError, setMonitorError] = useState('')
  const [monitorLoading, setMonitorLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    const controller = new AbortController()
    storageAPI
      .breakdown(controller.signal)
      .then((breakdown) => {
        if (!cancelled) setData(breakdown)
      })
      .catch((err: unknown) => {
        if (!cancelled) setStorageError((err as { message?: string })?.message ?? '无法获取存储数据')
      })
      .finally(() => {
        if (!cancelled) setStorageLoading(false)
      })
    return () => {
      cancelled = true
      controller.abort()
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    const tick = () =>
      statsAPI
        .monitor()
        .then((monitor) => {
          if (!cancelled) {
            setHardware(monitor)
            setLastMonitorAt(new Date().toISOString())
            setMonitorError('')
          }
        })
        .catch((err: unknown) => {
          if (!cancelled) setMonitorError((err as { message?: string })?.message ?? '实时监控暂不可用')
        })
        .finally(() => {
          if (!cancelled) setMonitorLoading(false)
        })
    tick()
    const id = window.setInterval(tick, 2_000)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [])

  const totalBytes = data?.total_bytes || 1
  const memPct = hardware && hardware.memory_total > 0
    ? (hardware.memory_used / hardware.memory_total) * 100
    : 0
  const diskPct = hardware && hardware.disk_total > 0
    ? (hardware.disk_used / hardware.disk_total) * 100
    : 0

  return (
    <div className="space-y-8">
      <StatsHeader lastMonitorAt={lastMonitorAt} monitorError={monitorError} />

      <section className="space-y-6">
        <h2 className="font-display text-xl font-semibold text-ink-600">存储概览</h2>
        {storageLoading ? (
          <p className="text-sand-500">存储数据加载中…</p>
        ) : !data ? (
          <p className="text-red-500">{storageError || '无法获取存储数据'}</p>
        ) : (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              <Tile icon={<HardDrive size={20} />} label="总占用" value={fmtBytes(data.total_bytes)} />
              <Tile icon={<Library size={20} />} label="媒体库" value={`${data.by_library.length}`} />
            </div>

            <div className="space-y-3">
              <h2 className="font-display text-xl font-semibold text-ink-600">按媒体库</h2>
              <div className="glass-panel overflow-x-auto">
                <table className="data-table min-w-[640px]">
                  <thead>
                    <tr>
                      <th>名称</th>
                      <th>类型</th>
                      <th>电影数</th>
                      <th>剧数</th>
                      <th>季数</th>
                      <th>集数</th>
                      <th>占用</th>
                      <th>占比</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.by_library.map((library) => {
                      const pct = (library.total_bytes / totalBytes) * 100
                      return (
                        <tr key={library.library_id}>
                          <td className="text-ink-600">{library.name}</td>
                          <td className="text-ink-100">{library.type}</td>
                          <td className="text-ink-100">{library.movie_count}</td>
                          <td className="text-ink-100">{library.series_count}</td>
                          <td className="text-ink-100">{library.season_count}</td>
                          <td className="text-ink-100">{library.episode_count}</td>
                          <td className="text-ink-100">{fmtBytes(library.total_bytes)}</td>
                          <td>
                            <div className="flex items-center gap-2">
                              <div className="h-1 w-24 overflow-hidden rounded bg-gray-200">
                                <div className="h-full bg-primary-400" style={{ width: `${pct.toFixed(1)}%` }} />
                              </div>
                              <span className="text-xs text-ink-50">{pct.toFixed(1)}%</span>
                            </div>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </div>

          </>
        )}
      </section>

      {monitorLoading && !hardware ? (
        <p className="text-sand-500">运行数据加载中…</p>
      ) : hardware ? (
        <SystemMonitorSection live={hardware} memPct={memPct} diskPct={diskPct} monitorError={monitorError} />
      ) : (
        <p className="text-red-500">{monitorError || '无法获取运行数据'}</p>
      )}
    </div>
  )
}

function Tile({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode
  label: string
  value: string
}) {
  return (
    <div className="glass-panel flex items-center gap-3 !p-4">
      <div className="rounded-xl border border-primary-400/40 bg-primary-400/10 p-2 text-brand-500">
        {icon}
      </div>
      <div>
        <p className="text-xs uppercase tracking-wider text-sand-500">{label}</p>
        <p className="font-display text-lg font-semibold text-ink-600">{value}</p>
      </div>
    </div>
  )
}
