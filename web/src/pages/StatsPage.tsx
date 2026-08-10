import { useEffect, useState } from 'react'

import { statsAPI } from '../api/stats'
import type { Hardware } from '../types'
import { StatsHeader, SystemMonitorSection } from './StatsPageSections'

// StatsPage polls live process and host metrics every 2 s.
export function StatsPage() {
  const [hardware, setHardware] = useState<Hardware | null>(null)
  const [lastMonitorAt, setLastMonitorAt] = useState<string>('')
  const [monitorError, setMonitorError] = useState('')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    statsAPI
      .snapshot()
      .then((snapshot) => {
        if (!cancelled) {
          setHardware(snapshot.hardware)
          setLastMonitorAt(snapshot.generated_at)
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    const tick = () =>
      statsAPI
        .monitor()
        .then((m) => {
          if (!cancelled) {
            setHardware(m)
            setLastMonitorAt(new Date().toISOString())
            setMonitorError('')
          }
        })
        .catch((err: unknown) => {
          if (!cancelled) {
            setMonitorError((err as { message?: string })?.message ?? '实时监控暂不可用')
          }
        })
    tick()
    const id = window.setInterval(tick, 2_000)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [])

  if (loading) return <p className="text-sand-500">加载中…</p>
  if (!hardware) return <p className="text-sand-500">无法获取运行数据</p>

  const live = hardware
  const memPct =
    live.memory_total > 0
      ? (live.memory_used / live.memory_total) * 100
      : 0
  const diskPct =
    live.disk_total > 0
      ? (live.disk_used / live.disk_total) * 100
      : 0

  return (
    <div className="space-y-8">
      <StatsHeader lastMonitorAt={lastMonitorAt} monitorError={monitorError} />
      <SystemMonitorSection
        live={live}
        memPct={memPct}
        diskPct={diskPct}
        monitorError={monitorError}
      />
    </div>
  )
}
