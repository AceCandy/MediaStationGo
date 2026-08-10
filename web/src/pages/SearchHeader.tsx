import { Sparkles } from 'lucide-react'

type SearchHeaderProps = {
  aiOn: boolean
  aiAvailable: boolean
  onToggleAI: () => void
}

export function SearchHeader({ aiOn, aiAvailable, onToggleAI }: SearchHeaderProps) {
  return (
    <header className="flex items-center justify-between">
      <h1 className="font-display text-3xl font-bold text-ink-600">搜索</h1>
      {aiAvailable && (
        <button
          className={
            'neon-button min-h-11 !px-3 !text-xs ' +
            (aiOn ? '!border-accent-400 !bg-accent-400/20 !text-accent-400' : '')
          }
          onClick={onToggleAI}
          title="切换 AI 智能搜索"
        >
          <Sparkles size={12} /> {aiOn ? '智能搜索已开启' : '智能搜索'}
        </button>
      )}
    </header>
  )
}
