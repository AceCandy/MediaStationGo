export type DownloadStatus = 'downloading' | 'verifying' | 'publishing' | 'waiting_verify' | 'failed' | 'queued' | 'cancelled' | 'completed'

export const statusLabels: Record<DownloadStatus, string> = { downloading: '↓ 下载中', verifying: '◉ 校验中', publishing: '↗ 发布中', waiting_verify: '◷ 等待校验', failed: '⚠ 失败', queued: '◷ 等待下载', cancelled: '⊘ 已取消', completed: '✓ 已完成' }
export const statusColors: Record<DownloadStatus, string> = { downloading: 'text-brand-500 bg-brand-500/10', verifying: 'text-sage-600 bg-sage-500/10', publishing: 'text-sage-600 bg-sage-500/10', waiting_verify: 'text-gold-600 bg-gold-500/10', failed: 'text-red-500 bg-red-500/10', queued: 'text-ink-50 bg-ink-100/5', cancelled: 'text-ink-50 bg-ink-100/5', completed: 'text-emerald-600 bg-emerald-500/10' }
