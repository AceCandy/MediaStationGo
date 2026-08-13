import { FormEvent, useState } from 'react'
import { KeyRound } from 'lucide-react'

import { ModalShell } from './ModalShell'

export type PasswordOptions = {
  title?: string
  message?: string
  confirmText?: string
}

export function PasswordDialog({
  options,
  onClose,
}: {
  options: PasswordOptions
  onClose: (value: string | null) => void
}) {
  const [password, setPassword] = useState('')

  const onSubmit = (event: FormEvent) => {
    event.preventDefault()
    if (!password) return
    onClose(password)
  }

  return (
    <ModalShell onClose={() => onClose(null)} maxWidth="max-w-sm" zIndex={110} ariaLabel={options.title || '需要密码确认'}>
      <form onSubmit={onSubmit}>
        <div className="flex gap-4 p-5">
          <div className="modal-icon">
            <KeyRound size={22} />
          </div>
          <div className="min-w-0 flex-1">
            <h3 className="font-display text-lg font-bold text-ink-600">
              {options.title || '需要密码确认'}
            </h3>
            <p className="mt-2 text-sm leading-6 text-ink-50">
              {options.message || '请输入当前账号密码以继续。'}
            </p>
            <input
              autoFocus
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              className="input-field mt-4"
              placeholder="当前账号密码"
              autoComplete="current-password"
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
            disabled={!password}
            className="btn-primary px-4 py-2"
          >
            {options.confirmText || '确认'}
          </button>
        </div>
      </form>
    </ModalShell>
  )
}
