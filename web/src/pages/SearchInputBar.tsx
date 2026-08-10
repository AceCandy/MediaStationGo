import type { ChangeEvent, FormEvent } from 'react'

type SearchInputBarProps = {
  aiOn: boolean
  query: string
  onQueryChange: (query: string) => void
  onAISubmit: (event: FormEvent) => void
}

export function SearchInputBar({ aiOn, query, onQueryChange, onAISubmit }: SearchInputBarProps) {
  if (aiOn) {
    return (
      <form onSubmit={onAISubmit} className="flex flex-wrap gap-2">
        <input
          className="input-base"
          name="ai-search"
          aria-label="AI 搜索描述"
          autoComplete="off"
          placeholder='例如：“2010 年后的科幻电影”或“最近的动漫”…'
          value={query}
          onChange={(e: ChangeEvent<HTMLInputElement>) => onQueryChange(e.target.value)}
        />
        <button type="submit" className="neon-button">
          搜索
        </button>
      </form>
    )
  }

  return (
    <input
      className="input-base"
      name="media-search"
      aria-label="按标题搜索媒体"
      autoComplete="off"
      placeholder="按标题搜索…"
      value={query}
      onChange={(e: ChangeEvent<HTMLInputElement>) => onQueryChange(e.target.value)}
    />
  )
}
