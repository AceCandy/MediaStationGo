import { useState } from 'react'
import toast from 'react-hot-toast'
import { Copy, ExternalLink, PlaySquare, X } from 'lucide-react'

import { playbackAPI, type ExternalPlayer } from '../api/playback'
import { ModalShell } from './ModalShell'

export function ExternalPlayerButton({
  mediaId,
  label = '外部播放器',
  compact = false,
}: {
  mediaId: string
  label?: string
  compact?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const [players, setPlayers] = useState<ExternalPlayer[]>([])
  const [streamURL, setStreamURL] = useState('')

  const load = async () => {
    setLoading(true)
    try {
      const playerData = await playbackAPI.externalPlayers(mediaId)
      setPlayers(playerData.players ?? [])
      setStreamURL(playerData.url ?? '')
      setOpen(true)
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '生成外部播放链接失败'
      toast.error(msg)
    } finally {
      setLoading(false)
    }
  }

  return (
    <>
      <button
        type="button"
        disabled={loading}
        onClick={(event) => {
          event.preventDefault()
          event.stopPropagation()
          load()
        }}
        className={
          compact
            ? 'rounded-lg border border-primary-400/35 bg-white px-2 py-1 text-xs font-semibold text-brand-500 hover:bg-primary-400/10 disabled:opacity-50'
            : 'btn-outline border-brand-500/30 px-5 text-[#c9954a] hover:border-brand-500 hover:bg-brand-50'
        }
      >
        <PlaySquare size={compact ? 13 : 14} className="mr-1 inline" />
        {loading ? '生成中…' : label}
      </button>
      {open && (
        <ExternalPlayerModal
          players={players}
          streamURL={streamURL}
          onClose={() => setOpen(false)}
        />
      )}
    </>
  )
}

function ExternalPlayerModal({
  players,
  streamURL,
  onClose,
}: {
  players: ExternalPlayer[]
  streamURL: string
  onClose: () => void
}) {
  const copy = async (value: string) => {
    await navigator.clipboard.writeText(value)
    toast.success('播放链接已复制')
  }

  return (
    <ModalShell onClose={onClose} maxWidth="max-w-2xl" zIndex={90} ariaLabel="外部播放器播放">
      <div className="modal-header p-5">
        <div>
          <h3 className="font-display text-xl font-bold text-ink-600">外部播放器播放</h3>
          <p className="mt-1 text-xs text-ink-50">链接已包含临时播放 Token，可复制到 VLC、Infuse、VidHub、SenPlayer 等客户端。</p>
        </div>
        <button onClick={onClose} className="icon-btn" aria-label="关闭">
          <X size={18} />
        </button>
      </div>
      <div className="space-y-4 p-5">
        <div className="rounded-2xl border border-gray-100 bg-gray-50 p-3">
          <div className="mb-2 text-xs font-semibold text-sand-500">直链播放地址</div>
          <div className="flex gap-2">
            <input readOnly value={streamURL} className="input-base min-w-0 flex-1 font-mono text-xs" />
            <button onClick={() => copy(streamURL)} className="btn-outline shrink-0 px-3">
              <Copy size={14} />
              复制
            </button>
          </div>
        </div>
        <div className="grid gap-2 sm:grid-cols-2">
          {players.map((player) => (
            <a
              key={player.name}
              href={player.url}
              className="flex items-center justify-between rounded-2xl border border-gray-100 bg-white px-4 py-3 text-sm font-semibold text-ink-600 shadow-sm transition-all duration-300 ease-smooth hover:-translate-y-0.5 hover:border-brand-300 hover:bg-brand-50 hover:shadow-card-hover"
            >
              <span>{player.name}</span>
              <ExternalLink size={15} className="text-brand-500" />
            </a>
          ))}
        </div>
      </div>
    </ModalShell>
  )
}
