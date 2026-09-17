import { useEffect, useRef, useState } from 'react'
import toast from 'react-hot-toast'
import { hongguoDownloadsAPI } from '../api/hongguoDownloads'

export function HongGuoDownloadButton({ sourceID, enabled }: { sourceID: string; enabled: boolean }) {
  const [busy, setBusy] = useState(false)
  const active = useRef(true)
  useEffect(() => { active.current = true; return () => { active.current = false } }, [])
  return <div className="flex flex-wrap gap-2">
    <button className="btn-primary" disabled={busy || !enabled} onClick={() => {
      if (busy) return
      setBusy(true)
      void hongguoDownloadsAPI.enqueue(sourceID).then((result) => {
        if (active.current) toast.success(result.added ? `已创建 ${result.added} 集任务，请在下载空间查看` : '该作品分集任务已存在，可在下载空间重试失败项')
      }).catch((err: unknown) => {
        if (active.current) toast.error((err as { response?: { data?: { error?: string } } })?.response?.data?.error || '创建下载失败')
      }).finally(() => { if (active.current) setBusy(false) })
    }}>{busy ? '创建任务中…' : '下载已更新分集'}</button>
  </div>
}
