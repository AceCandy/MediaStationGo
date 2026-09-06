import { useRef, useState } from 'react'
import { AlertTriangle } from 'lucide-react'
import toast from 'react-hot-toast'

import { mediaAPI, type STRMDeleteTarget } from '../api/library'
import { ModalShell } from './ModalShell'

export function STRMDeleteDialog({ mediaID, target, onClose, onDeleted }: {
  mediaID: string
  target: STRMDeleteTarget
  onClose: () => void
  onDeleted: () => void
}) {
  const [deleteParent, setDeleteParent] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const pending = useRef(false)
  const deletePath = deleteParent && target.parent_path ? target.parent_path : target.target_path

  const handleDelete = async () => {
    if (pending.current) return
    pending.current = true
    setDeleting(true)
    try {
      await mediaAPI.deleteSTRMTarget(mediaID, deleteParent)
      toast.success(deleteParent ? '父目录已删除' : '本地文件已删除')
      onDeleted()
    } catch (err: unknown) {
      const message = (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '删除失败'
      toast.error(message)
    } finally {
      pending.current = false
      setDeleting(false)
    }
  }

  return (
    <ModalShell onClose={deleting ? undefined : onClose} maxWidth="max-w-lg" zIndex={100} ariaLabel="确认删除 STRM 本地目标">
      <div className="flex gap-4 p-5">
        <div className="modal-icon modal-icon--danger">
          <AlertTriangle size={22} aria-hidden="true" />
        </div>
        <div className="min-w-0 flex-1">
          <h3 className="font-display text-lg font-bold text-ink-600">确认删除本地路径</h3>
          <p className="mt-2 text-sm leading-6 text-ink-50">此操作不可恢复，仅删除下面的本地路径。STRM 文件和媒体记录将保留。</p>
          <p className="mt-4 text-xs font-bold text-[var(--app-muted)]">最终将删除的本地绝对路径</p>
          <p className="mt-1 break-all rounded-xl border border-red-200 bg-red-50 p-3 font-mono text-xs text-red-700">{deletePath}</p>
          <label className="mt-4 flex items-start gap-2 text-sm text-ink-100">
            <input type="checkbox" className="mt-0.5" checked={deleteParent} disabled={!target.parent_path || deleting} onChange={(event) => setDeleteParent(event.target.checked)} />
            <span>
              删除父目录及其全部内容
              {!target.parent_path && <span className="ml-1 text-xs text-ink-50">（该父目录不可安全删除）</span>}
            </span>
          </label>
        </div>
      </div>
      <div className="modal-footer">
        <button type="button" className="btn-outline px-4 py-2 shadow-none" disabled={deleting} onClick={onClose}>取消</button>
        <button type="button" className="btn-danger px-4 py-2" disabled={deleting} onClick={handleDelete}>{deleting ? '删除中…' : '确认删除'}</button>
      </div>
    </ModalShell>
  )
}
