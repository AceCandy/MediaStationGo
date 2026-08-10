import { ArrowLeft, Cast } from 'lucide-react'
import { Link } from 'react-router-dom'

type PlayerTopBarProps = {
  onBack: () => void
  castTo?: string
}

export function PlayerTopBar({ onBack, castTo }: PlayerTopBarProps) {
  return (
    <div className="pointer-events-none absolute inset-x-0 top-0 z-20 flex items-center justify-between gap-3 p-4 sm:p-6">
      <button
        onClick={onBack}
        className="pointer-events-auto flex items-center gap-2 rounded-full border border-white/15 bg-black/70 px-4 py-2 text-sm font-medium text-white shadow-xl backdrop-blur transition hover:bg-black/85"
      >
        <ArrowLeft size={16} /> 返回
      </button>
      {castTo && (
        <Link
          to={castTo}
          aria-label="投屏当前媒体"
          title="投屏当前媒体"
          className="pointer-events-auto flex min-h-11 items-center gap-2 rounded-full border border-white/15 bg-black/70 px-4 text-sm font-medium text-white shadow-xl backdrop-blur transition hover:bg-black/85"
        >
          <Cast size={16} />
          投屏
        </Link>
      )}
    </div>
  )
}
