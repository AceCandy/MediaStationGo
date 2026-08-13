import { AlertTriangle } from 'lucide-react'
import clsx from 'clsx'

import { ModalShell } from './ModalShell'

export type ConfirmOptions = {
  title?: string
  message: string
  confirmText?: string
  cancelText?: string
  danger?: boolean
}

export function ConfirmDialog({
  options,
  onClose,
}: {
  options: ConfirmOptions
  onClose: (value: boolean) => void
}) {
  const danger = options.danger ?? true
  return (
    <ModalShell onClose={() => onClose(false)} maxWidth="max-w-md" zIndex={100} ariaLabel={options.title || '确认操作'}>
      <div className="flex gap-4 p-5">
        <div className={clsx('modal-icon', danger && 'modal-icon--danger')}>
          <AlertTriangle size={22} />
        </div>
        <div className="min-w-0 flex-1">
          <h3 className="font-display text-lg font-bold text-ink-600">
            {options.title || '确认操作'}
          </h3>
          <p className="mt-2 text-sm leading-6 text-ink-50">{options.message}</p>
        </div>
      </div>
      <div className="modal-footer">
        <button
          type="button"
          onClick={() => onClose(false)}
          className="btn-outline px-4 py-2 shadow-none"
        >
          {options.cancelText || '取消'}
        </button>
        <button
          type="button"
          onClick={() => onClose(true)}
          className={clsx(danger ? 'btn-danger' : 'btn-primary', 'px-4 py-2')}
        >
          {options.confirmText || '确认'}
        </button>
      </div>
    </ModalShell>
  )
}
