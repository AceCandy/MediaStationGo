import { useEffect, type ReactNode } from 'react'
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

  useEffect(() => {
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    if (!onClose) {
      return () => {
        document.body.style.overflow = previousOverflow
      }
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        onClose()
      }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.body.style.overflow = previousOverflow
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [onClose])

  return (
    <motion.div
      className="modal-backdrop"
      style={{ zIndex }}
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ duration: reduceMotion ? 0.01 : 0.18 }}
      onClick={onClose}
    >
      <motion.div
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
    </motion.div>
  )
}
