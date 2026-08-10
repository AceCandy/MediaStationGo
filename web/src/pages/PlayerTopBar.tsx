import { ArrowLeft } from 'lucide-react'

type PlayerTopBarProps = {
  onBack: () => void
}

export function PlayerTopBar({ onBack }: PlayerTopBarProps) {
  return (
    <div className="pointer-events-none absolute inset-x-0 top-0 z-20 flex items-center p-4 sm:p-6">
      <button
        onClick={onBack}
        className="pointer-events-auto flex items-center gap-2 rounded-full border border-white/15 bg-black/70 px-4 py-2 text-sm font-medium text-white shadow-xl backdrop-blur transition hover:bg-black/85"
      >
        <ArrowLeft size={16} /> 返回
      </button>
    </div>
  )
}
