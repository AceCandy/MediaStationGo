import { useId, type ReactNode } from 'react'
import { NavLink } from 'react-router-dom'
import { AnimatePresence, motion } from 'framer-motion'
import { ChevronDown } from 'lucide-react'
import clsx from 'clsx'

type SidebarGroupProps = {
  id: string
  icon: ReactNode
  label: string
  children: ReactNode
  collapsed?: boolean
  open?: boolean
  active?: boolean
  onToggle: (id: string) => void
}

export function SidebarGroup({ id, icon, label, children, collapsed, open, active, onToggle }: SidebarGroupProps) {
  const contentId = useId()

  return (
    <div className="space-y-1">
      <button
        type="button"
        onClick={() => onToggle(id)}
        aria-label={collapsed ? label : undefined}
        aria-expanded={collapsed ? undefined : Boolean(open)}
        aria-controls={collapsed ? undefined : contentId}
        className={clsx(
          'group relative flex w-full items-center gap-3 rounded-xl px-3.5 py-2.5 text-[13px] font-bold transition-all duration-300 ease-smooth',
          active
            ? 'text-white shadow-glow-sm'
            : 'text-[var(--app-muted)] hover:bg-[var(--app-hover)] hover:text-[var(--app-text)]',
          collapsed && 'justify-center px-0',
        )}
        style={active ? { background: 'linear-gradient(135deg, #8b5cf6 0%, #7c3aed 60%, #6d28d9 100%)' } : undefined}
      >
        <span className={clsx(
          'flex h-5 w-5 shrink-0 items-center justify-center transition-transform duration-300 group-hover:scale-110',
          active ? 'text-white' : 'text-[var(--app-muted)] group-hover:text-[var(--app-brand-text)]',
        )}>
          {icon}
        </span>
        {!collapsed && (
          <>
            <span className="flex-1 truncate text-left">{label}</span>
            <ChevronDown
              size={14}
              className={clsx('transition-transform duration-200', open && 'rotate-180')}
            />
          </>
        )}
        {collapsed && (
          <div className="absolute left-full z-50 ml-3 rounded-xl bg-[var(--app-tooltip-bg)] px-2.5 py-1.5 text-xs font-semibold text-[var(--app-tooltip-text)] opacity-0 shadow-lg transition-opacity group-hover:opacity-100">
            {label}
          </div>
        )}
      </button>
      <div id={contentId}>
        <AnimatePresence initial={false}>
          {!collapsed && open && (
            <motion.div
              initial={{ height: 0, opacity: 0 }}
              animate={{ height: 'auto', opacity: 1 }}
              exit={{ height: 0, opacity: 0 }}
              transition={{ duration: 0.22, ease: [0.21, 0.47, 0.32, 0.98] }}
              className="overflow-hidden"
            >
              <div className="space-y-0.5 pb-1 pl-2 pt-1">
                {children}
              </div>
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </div>
  )
}

type SidebarLinkProps = {
  to: string
  icon: ReactNode
  label: string
  end?: boolean
  active?: boolean
  collapsed?: boolean
  child?: boolean
}

export function SidebarLink({ to, icon, label, end, active = false, collapsed, child }: SidebarLinkProps) {
  return (
    <NavLink
      to={to}
      end={end}
      aria-current={active ? 'page' : undefined}
      className={({ isActive }) =>
        clsx(
          'relative flex items-center gap-3 rounded-xl px-3.5 py-2.5 text-[13px] font-semibold transition-all duration-300 ease-smooth group',
          child && 'py-2 text-xs',
          isActive || active
            ? 'bg-[var(--app-brand-soft)] text-[var(--app-brand-text)]'
            : 'text-[var(--app-muted)] hover:bg-[var(--app-hover)] hover:text-[var(--app-text)]',
        )
      }
    >
      {({ isActive }) => (
        <>
          {(isActive || active) && (
            <span
              className="absolute left-0 top-1/2 h-4 w-[3px] -translate-y-1/2 rounded-full"
              style={{ background: 'linear-gradient(180deg, #a78bfa, #7c3aed)' }}
            />
          )}
          <span className={clsx(
            'flex h-5 w-5 shrink-0 items-center justify-center transition-all duration-300 group-hover:scale-110',
            isActive || active ? 'text-[var(--app-brand-text)]' : 'text-[var(--app-muted)] group-hover:text-[var(--app-brand-text)]',
          )}>
            {icon}
          </span>
          {!collapsed && (
            <motion.span
              initial={{ opacity: 0, x: -5 }}
              animate={{ opacity: 1, x: 0 }}
              className="truncate whitespace-nowrap"
            >
              {label}
            </motion.span>
          )}
          {collapsed && (
            <div className="pointer-events-none absolute left-full z-50 ml-3 whitespace-nowrap rounded-xl bg-[var(--app-tooltip-bg)] px-2.5 py-1.5 text-xs font-semibold text-[var(--app-tooltip-text)] opacity-0 shadow-lg transition-opacity group-hover:pointer-events-auto group-hover:opacity-100">
              {label}
            </div>
          )}
        </>
      )}
    </NavLink>
  )
}
