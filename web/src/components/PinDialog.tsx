import { FormEvent, useState } from 'react'
import { LockKeyhole } from 'lucide-react'

import { ModalShell } from './ModalShell'

export type PinOptions = {
  title?: string
  message?: string
  profileName: string
}

export function PinDialog({
  options,
  onClose,
}: {
  options: PinOptions
  onClose: (value: string | null) => void
}) {
  const [pin, setPin] = useState('')

  const onSubmit = (event: FormEvent) => {
    event.preventDefault()
    const trimmed = pin.trim()
    if (!trimmed) return
    onClose(trimmed)
  }

  return (
    <ModalShell onClose={() => onClose(null)} maxWidth="max-w-sm" zIndex={110} ariaLabel={options.title || '需要 PIN 验证'}>
      <form onSubmit={onSubmit}>
        <div className="flex gap-4 p-5">
          <div className="modal-icon modal-icon--gold">
            <LockKeyhole size={22} />
          </div>
          <div className="min-w-0 flex-1">
            <h3 className="font-display text-lg font-bold text-ink-600">
              {options.title || '需要 PIN 验证'}
            </h3>
            <p className="mt-2 text-sm leading-6 text-ink-50">
              {options.message || `切换到「${options.profileName}」前请输入 PIN。`}
            </p>
            <input
              autoFocus
              type="password"
              inputMode="numeric"
              minLength={4}
              maxLength={8}
              value={pin}
              onChange={(event) => setPin(event.target.value)}
              className="input-field mt-4 text-center text-lg font-bold tracking-[0.35em]"
              placeholder="••••"
            />
          </div>
        </div>
        <div className="modal-footer">
          <button
            type="button"
            onClick={() => onClose(null)}
            className="btn-outline px-4 py-2 shadow-none"
          >
            取消
          </button>
          <button
            type="submit"
            disabled={!pin.trim()}
            className="btn-primary px-4 py-2"
          >
            验证并切换
          </button>
        </div>
      </form>
    </ModalShell>
  )
}
