import { Link } from 'react-router-dom'
import { ArrowRight } from 'lucide-react'

type ShortcutItem = {
  to: string
  title: string
  description: string
  badge?: string
}

type ManagementShortcutsProps = {
  title: string
  description?: string
  items: ShortcutItem[]
}

export function ManagementShortcuts({ title, description, items }: ManagementShortcutsProps) {
  return (
    <section className="rounded-3xl border border-gray-200 bg-white p-5 shadow-sm">
      <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="font-display text-lg font-bold text-ink-600">{title}</h2>
          {description && <p className="mt-1 text-sm text-ink-50">{description}</p>}
        </div>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {items.map((item) => (
          <Link
            key={item.to}
            to={item.to}
            className="group rounded-2xl border border-gray-200 bg-gray-50/70 p-4 transition hover:-translate-y-0.5 hover:border-primary-400/50 hover:bg-white hover:shadow-md"
          >
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <h3 className="truncate text-sm font-bold text-ink-600">{item.title}</h3>
                  {item.badge && (
                    <span className="shrink-0 rounded-full bg-primary-400/10 px-2 py-0.5 text-[10px] font-bold text-brand-500">
                      {item.badge}
                    </span>
                  )}
                </div>
                <p className="mt-2 line-clamp-2 text-xs leading-5 text-ink-50">
                  {item.description}
                </p>
              </div>
              <ArrowRight
                size={16}
                className="mt-0.5 shrink-0 text-sand-500 transition group-hover:translate-x-0.5 group-hover:text-brand-500"
              />
            </div>
          </Link>
        ))}
      </div>
    </section>
  )
}
