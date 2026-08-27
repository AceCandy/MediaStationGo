import {
  Children,
  isValidElement,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactElement,
  type ReactNode,
} from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown } from 'lucide-react'

type OptionProps = {
  children?: ReactNode
  disabled?: boolean
  value?: string | number
}

type SelectOption = {
  disabled: boolean
  label: ReactNode
  value: string
}

type SelectProps = {
  children: ReactNode
  className?: string
  disabled?: boolean
  name?: string
  required?: boolean
  title?: string
  value: string | number
  onChange: (value: string) => void
  'aria-label'?: string
}

function enabledIndex(options: SelectOption[], start: number, step: 1 | -1): number {
  if (options.length === 0) return -1
  for (let offset = 1; offset <= options.length; offset++) {
    const index = (start + step * offset + options.length) % options.length
    if (!options[index].disabled) return index
  }
  return -1
}

/** Select 用主题化弹层替代浏览器原生下拉，同时保留 option 声明方式。 */
export function Select({
  children,
  className = '',
  disabled = false,
  name,
  required = false,
  title,
  value,
  onChange,
  'aria-label': ariaLabel,
}: SelectProps) {
  const options = Children.toArray(children).flatMap((child): SelectOption[] => {
    if (!isValidElement(child) || child.type !== 'option') return []
    const props = (child as ReactElement<OptionProps>).props
    return [{ disabled: Boolean(props.disabled), label: props.children, value: String(props.value ?? '') }]
  })
  const selectedIndex = options.findIndex((option) => option.value === String(value))
  const [open, setOpen] = useState(false)
  const [activeIndex, setActiveIndex] = useState(selectedIndex)
  const [menuStyle, setMenuStyle] = useState({ left: 0, top: 0, width: 0, maxHeight: 256 })
  const buttonRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const listboxID = useId()

  useEffect(() => {
    if (disabled) setOpen(false)
  }, [disabled])

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Node
      if (!buttonRef.current?.contains(target) && !menuRef.current?.contains(target)) setOpen(false)
    }
    document.addEventListener('pointerdown', onPointerDown)
    return () => document.removeEventListener('pointerdown', onPointerDown)
  }, [open])

  useLayoutEffect(() => {
    if (!open) return
    const updatePosition = () => {
      const rect = buttonRef.current?.getBoundingClientRect()
      if (!rect) return
      const gap = 6
      const below = window.innerHeight - rect.bottom - gap
      const above = rect.top - gap
      const maxHeight = Math.max(96, Math.min(256, Math.max(below, above) - 8))
      setMenuStyle({
        left: Math.max(8, Math.min(rect.left, window.innerWidth - rect.width - 8)),
        top: below >= Math.min(192, above) ? rect.bottom + gap : Math.max(8, rect.top - maxHeight - gap),
        width: rect.width,
        maxHeight,
      })
    }
    updatePosition()
    window.addEventListener('resize', updatePosition)
    window.addEventListener('scroll', updatePosition, true)
    return () => {
      window.removeEventListener('resize', updatePosition)
      window.removeEventListener('scroll', updatePosition, true)
    }
  }, [open])

  useEffect(() => {
    if (!open || activeIndex < 0) return
    menuRef.current?.querySelector<HTMLElement>(`[data-option-index="${activeIndex}"]`)?.focus()
  }, [activeIndex, open])

  const openMenu = (direction: 1 | -1 = 1) => {
    const initial = selectedIndex >= 0 && !options[selectedIndex].disabled
      ? selectedIndex
      : enabledIndex(options, direction === 1 ? -1 : 0, direction)
    setActiveIndex(initial)
    setOpen(true)
  }

  const choose = (index: number) => {
    const option = options[index]
    if (!option || option.disabled) return
    onChange(option.value)
    setOpen(false)
    buttonRef.current?.focus()
  }

  const onButtonKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
    event.preventDefault()
    openMenu(event.key === 'ArrowDown' ? 1 : -1)
  }

  const onMenuKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape' || event.key === 'Tab') {
      setOpen(false)
      if (event.key === 'Escape') {
        event.preventDefault()
        buttonRef.current?.focus()
      }
      return
    }
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      choose(activeIndex)
      return
    }
    if (event.key === 'Home' || event.key === 'End') {
      event.preventDefault()
      setActiveIndex(enabledIndex(options, event.key === 'Home' ? -1 : 0, event.key === 'Home' ? 1 : -1))
      return
    }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      setActiveIndex((current) => enabledIndex(options, current, event.key === 'ArrowDown' ? 1 : -1))
    }
  }

  const selected = options[selectedIndex]
  return (
    <div className="relative min-w-0">
      <button
        ref={buttonRef}
        type="button"
        className={`flex items-center justify-between gap-2 text-left disabled:cursor-not-allowed disabled:opacity-50 ${className}`}
        disabled={disabled}
        title={title}
        aria-label={ariaLabel}
        aria-controls={open ? listboxID : undefined}
        aria-expanded={open}
        aria-haspopup="listbox"
        onClick={() => open ? setOpen(false) : openMenu()}
        onKeyDown={onButtonKeyDown}
      >
        <span className="min-w-0 flex-1 truncate">{selected?.label}</span>
        <ChevronDown size={16} className={`shrink-0 transition-transform ${open ? 'rotate-180' : ''}`} aria-hidden="true" />
      </button>
      {required && (
        <input
          className="pointer-events-none absolute size-px opacity-0"
          tabIndex={-1}
          name={name}
          value={String(value)}
          required
          disabled={disabled}
          aria-hidden="true"
          onChange={() => undefined}
          onInvalid={() => buttonRef.current?.focus()}
        />
      )}
      {open && createPortal(
        <div
          ref={menuRef}
          id={listboxID}
          role="listbox"
          aria-label={ariaLabel}
          className="fixed z-[200] overflow-y-auto rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)] p-1.5 shadow-xl"
          style={menuStyle}
          onKeyDown={onMenuKeyDown}
        >
          {options.map((option, index) => (
            <button
              key={`${option.value}-${index}`}
              type="button"
              role="option"
              aria-selected={index === selectedIndex}
              data-option-index={index}
              disabled={option.disabled}
              className={`flex w-full items-center gap-2 rounded-lg px-3 py-2.5 text-left text-sm font-semibold transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${index === selectedIndex ? 'bg-brand-500/15 text-brand-500' : 'text-[var(--app-text)] hover:bg-[var(--app-hover)]'}`}
              onClick={() => choose(index)}
            >
              <span className="min-w-0 flex-1 truncate">{option.label}</span>
              {index === selectedIndex && <Check size={16} className="shrink-0" aria-hidden="true" />}
            </button>
          ))}
        </div>,
        document.body,
      )}
    </div>
  )
}
