import type { ActiveTranscode } from '../api/tasks'

export function TranscodeTaskTable({ transcodes }: { transcodes: ActiveTranscode[] }) {
  if (transcodes.length === 0) return <p className="text-sand-500">暂无运行中转码。</p>
  return (
    <table className="w-full text-left text-sm">
      <thead className="text-xs uppercase tracking-wider text-sand-500">
        <tr>
          <th className="py-2">媒体 ID</th>
          <th>音轨</th>
          <th>编码器</th>
          <th>开始时间</th>
          <th>就绪</th>
        </tr>
      </thead>
      <tbody>
        {transcodes.map((t) => (
          <tr key={t.job_id} className="border-t border-gray-200">
            <td className="py-2 font-mono text-xs text-ink-600">{t.media_id}</td>
            <td className="text-ink-100">{t.audio_stream_index >= 0 ? t.audio_stream_index : '默认'}</td>
            <td className="text-ink-100">{t.encoder || 'libx264'}</td>
            <td className="text-ink-100">{new Date(t.started_at).toLocaleTimeString()}</td>
            <td>
              {t.playlist_ok ? (
                <span className="rounded-lg border border-emerald-400/40 px-1.5 py-0.5 text-xs text-emerald-400">
                  ready
                </span>
              ) : (
                <span className="rounded-lg border border-yellow-400/40 px-1.5 py-0.5 text-xs text-yellow-400">
                  starting
                </span>
              )}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
