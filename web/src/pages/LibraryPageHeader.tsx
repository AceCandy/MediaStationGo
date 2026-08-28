import type { Library } from '../types'
import { libraryDisplayPath } from './libraryDisplayModel'

type LibraryPageHeaderProps = {
  library: Library | null
  itemCount: number
  loadingAllText: string
  isAdmin: boolean
  missingPoster: boolean
  missingChineseTitle: boolean
  onMissingPosterChange: (checked: boolean) => void
  onMissingChineseTitleChange: (checked: boolean) => void
}

export function LibraryPageHeader({
  library,
  itemCount,
  loadingAllText,
  isAdmin,
  missingPoster,
  missingChineseTitle,
  onMissingPosterChange,
  onMissingChineseTitleChange,
}: LibraryPageHeaderProps) {
  const displayPath = library ? libraryDisplayPath(library.path) : ''

  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h1 className="font-display text-3xl font-bold text-ink-600">
          {library?.name ?? '媒体库'}
          <span className="text-sand-500"> ({itemCount})</span>
        </h1>
        {library && <p className="text-sm text-ink-50" title={library.path}>{library.type} · {displayPath}</p>}
        {loadingAllText && <p className="mt-1 text-xs text-sand-500">{loadingAllText}</p>}
      </div>
      {isAdmin && (
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            aria-pressed={missingPoster}
            onClick={() => onMissingPosterChange(!missingPoster)}
            className={`btn-outline ${missingPoster ? 'border-brand-500 text-brand-500' : ''}`}
          >
            无海报
          </button>
          <button
            type="button"
            aria-pressed={missingChineseTitle}
            onClick={() => onMissingChineseTitleChange(!missingChineseTitle)}
            className={`btn-outline ${missingChineseTitle ? 'border-brand-500 text-brand-500' : ''}`}
          >
            无中文名
          </button>
        </div>
      )}
    </div>
  )
}
