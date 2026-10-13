import { useEffect, useRef, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { motion, useReducedMotion } from 'framer-motion'
import clsx from 'clsx'

type ModalShellProps = {
  /** 传入后启用：点击遮罩关闭 + Escape 关闭。表单类弹窗可不传，避免误关丢失输入。 */
  onClose?: () => void
  /** 面板宽度类，如 'max-w-md' */
  maxWidth?: string
  /** 面板附加类（内边距、滚动等） */
  className?: string
  /** 遮罩层 z-index，默认 50；确认/密码类二级弹窗用更高值 */
  zIndex?: number
  ariaLabel?: string
  children: ReactNode
}

const dialogs: { panel: HTMLElement; zIndex: number; returnFocus: HTMLElement | null }[] = []
let bodyOverflow = ''
const topDialog = () => dialogs.reduce<typeof dialogs[number] | undefined>((top, value) => !top || value.zIndex >= top.zIndex ? value : top, undefined)
const focusableElements = (panel: HTMLElement) => Array.from(panel.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex]:not([tabindex="-1"])')).filter(element => element.tabIndex >= 0 && element.getClientRects().length > 0 && !element.closest('[inert], [aria-hidden="true"]'))

/**
 * 全站统一弹窗壳：主题化遮罩（modal-backdrop）+ 玻璃面板（modal-panel）+
 * 弹簧入场动效（scale/translate/opacity），并处理滚动锁定与 Escape/遮罩关闭。
 * 退出动效依赖调用方条件渲染，当前仅做入场。
 */
export function ModalShell({
  onClose,
  maxWidth = 'max-w-md',
  className,
  zIndex = 50,
  ariaLabel,
  children,
}: ModalShellProps) {
  const reduceMotion = useReducedMotion()
  const panel = useRef<HTMLDivElement>(null)
  // 在挂载前记录发起控件，避免子组件自动聚焦和 StrictMode 重放把弹窗内部当成发起者。
  const opener = useRef(document.activeElement instanceof HTMLElement ? document.activeElement : null)
  const fallbackFocus = useRef(topDialog()?.returnFocus || null)
  const close = useRef(onClose)
  useEffect(() => { close.current = onClose }, [onClose])

  useEffect(() => {
    const node = panel.current
    if (!node) return
    const previousFocus = opener.current
    const fallback = fallbackFocus.current
    const entry = { panel: node, zIndex, returnFocus: previousFocus?.isConnected ? previousFocus : fallback }
    if (dialogs.length === 0) bodyOverflow = document.body.style.overflow
    dialogs.push(entry)
    document.body.style.overflow = 'hidden'
    const focusFirst = () => (focusableElements(node)[0] || node).focus()
    if (topDialog() === entry && !node.contains(document.activeElement)) focusFirst()
    const handleKeyDown = (event: KeyboardEvent) => {
      if (topDialog() !== entry) return
      if (event.key === 'Escape') {
        event.preventDefault()
        event.stopImmediatePropagation()
        close.current?.()
      } else if (event.key === 'Tab') {
        const elements = focusableElements(node)
        const first = elements[0], last = elements[elements.length - 1]
        if (!first) { event.preventDefault(); node.focus() }
        else if (event.shiftKey && (document.activeElement === first || !node.contains(document.activeElement) || document.activeElement === node)) { event.preventDefault(); last.focus() }
        else if (!event.shiftKey && (document.activeElement === last || !node.contains(document.activeElement) || document.activeElement === node)) { event.preventDefault(); first.focus() }
      }
    }
    const handleFocus = (event: FocusEvent) => {
      if (topDialog() === entry && !node.contains(event.target as Node)) focusFirst()
    }
    document.addEventListener('keydown', handleKeyDown, true)
    document.addEventListener('focusin', handleFocus)
    return () => {
      const wasTop = topDialog() === entry
      dialogs.splice(dialogs.indexOf(entry), 1)
      document.removeEventListener('keydown', handleKeyDown, true)
      document.removeEventListener('focusin', handleFocus)
      if (dialogs.length === 0) document.body.style.overflow = bodyOverflow
      if (wasTop) {
        const next = topDialog()
        const target = previousFocus?.isConnected ? previousFocus : fallback
        if (target?.isConnected && (!next || next.panel.contains(target))) target.focus()
        else if (next) (focusableElements(next.panel)[0] || next.panel).focus()
      }
    }
  }, [zIndex])

  // Portal 到 body：路由容器带有 transform 动效，会让 fixed 蒙层相对它定位而非视口
  return createPortal(
    <motion.div
      className="modal-backdrop"
      style={{ zIndex }}
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ duration: reduceMotion ? 0.01 : 0.18 }}
      onClick={onClose}
    >
      <motion.div
        ref={panel}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-label={ariaLabel}
        className={clsx('modal-panel', maxWidth, className)}
        initial={reduceMotion ? { opacity: 0 } : { opacity: 0, scale: 0.94, y: 18 }}
        animate={reduceMotion ? { opacity: 1 } : { opacity: 1, scale: 1, y: 0 }}
        transition={
          reduceMotion
            ? { duration: 0.01 }
            : { type: 'spring', stiffness: 420, damping: 32, mass: 0.9 }
        }
        onClick={(event) => event.stopPropagation()}
      >
        {children}
      </motion.div>
    </motion.div>,
    document.body,
  )
}
