import { motion } from 'framer-motion'
import { Search, SearchX } from 'lucide-react'
import type { ReactNode } from 'react'

type SearchStatusPanelsProps = {
  loading: boolean
  error: string
  showIdle: boolean
  showEmpty: boolean
}

export function SearchStatusPanels({
  loading,
  error,
  showIdle,
  showEmpty,
}: SearchStatusPanelsProps) {
  return (
    <>
      {loading && (
        <div className="flex items-center gap-2 py-8 text-ink-50">
          <span className="inline-block h-4 w-4 animate-spin rounded-full border-2 border-primary-400 border-t-transparent" />
          搜索中…
        </div>
      )}

      {error && (
        <div className="glass-panel !border-red-400/30 p-4 text-sm text-red-400">{error}</div>
      )}

      {showIdle && (
        <StatusPanel
          icon={<Search size={26} className="stroke-[1.75]" />}
          title="输入关键词开始搜索"
          subtitle="支持电影、电视剧、动漫等媒体内容的快速搜索"
        />
      )}

      {showEmpty && (
        <StatusPanel
          icon={<SearchX size={26} className="stroke-[1.75]" />}
          title="未找到匹配的媒体"
          subtitle="尝试其他关键词，或者添加媒体库后执行扫描"
        />
      )}
    </>
  )
}

function StatusPanel({ icon, title, subtitle }: { icon: ReactNode; title: string; subtitle: string }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.45, ease: [0.21, 0.47, 0.32, 0.98] }}
      className="glass-panel relative flex flex-col items-center gap-3 overflow-hidden p-14 text-center"
    >
      {/* 背景氛围光斑 */}
      <div aria-hidden="true" className="pointer-events-none absolute -top-20 left-1/2 h-48 w-96 -translate-x-1/2 rounded-full bg-brand-500/10 blur-3xl" />
      <div className="relative flex h-16 w-16 items-center justify-center rounded-3xl border border-[var(--app-brand-border)] bg-[var(--app-brand-soft)] text-[var(--app-accent)] shadow-glow-sm">
        {icon}
      </div>
      <p className="relative font-display text-xl font-extrabold tracking-tight text-gradient-brand">{title}</p>
      <p className="relative text-sm text-[var(--app-muted)]">{subtitle}</p>
    </motion.div>
  )
}
