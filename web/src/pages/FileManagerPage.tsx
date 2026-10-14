import { useCallback, useEffect, useMemo, useState } from 'react'
import { ChevronUp, Home } from 'lucide-react'

import { filesAPI, type FileEntry, type FileListing } from '../api/files'
import { FileBrowserRoots } from './FileBrowserRoots'
import { FileEntriesTable } from './FileEntriesTable'
import { FileManagerToolbar } from './FileManagerToolbar'
import { FileOperationsPanel } from './FileOperationsPanel'
import { useFileOperations } from './useFileOperations'

// FileManagerPage provides a focused local storage view:
// browse allowed roots, optionally recurse, and perform safe local operations.
export function FileManagerPage() {
  const [path, setPath] = useState('')
  const [data, setData] = useState<FileListing | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [recursive, setRecursive] = useState(false)

  const currentDir = useMemo(() => {
    if (data?.path) return data.path
    return path
  }, [data?.path, path])
  const refresh = useCallback(() => {
    setLoading(true)
    setError('')
    filesAPI
      .list(path, recursive ? 5000 : 1000, recursive)
      .then(setData)
      .catch((err: unknown) => {
        const msg =
          (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
          '加载失败'
        setError(msg)
      })
      .finally(() => setLoading(false))
  }, [path, recursive])

  useEffect(() => {
    refresh()
  }, [refresh])

  const fileOperations = useFileOperations({ currentDir, path, refresh })
  const enter = (e: FileEntry) => {
    if (e.is_dir) setPath(e.path)
  }

  return (
    <div className="space-y-6">
      <FileManagerToolbar
        currentPath={data?.path}
        recursive={recursive}
        onRefresh={refresh}
        onRecursiveChange={setRecursive}
      />

      {data?.path && (
        <FileOperationsPanel
          selected={fileOperations.selected}
          folderName={fileOperations.folderName}
          renameTo={fileOperations.renameTo}
          destPath={fileOperations.destPath}
          transferMode={fileOperations.transferMode}
          busy={fileOperations.busy}
          onFolderNameChange={fileOperations.setFolderName}
          onRenameToChange={fileOperations.setRenameTo}
          onDestPathChange={fileOperations.setDestPath}
          onTransferModeChange={fileOperations.setTransferMode}
          onCreateFolder={fileOperations.createFolder}
          onRenameSelected={fileOperations.renameSelected}
          onDeleteSelected={fileOperations.deleteSelected}
          onTransferSelected={fileOperations.transferSelected}
        />
      )}

      {loading && <p className="text-sand-500">加载中…</p>}
      {error && <div className="glass-panel !border-red-400/40 text-red-400">{error}</div>}

      {!loading && data && !data.path && data.roots && (
        <FileBrowserRoots roots={data.roots} onOpen={setPath} />
      )}

      {!loading && data?.path && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] px-3 py-2 shadow-sm">
          <div className="flex flex-wrap items-center gap-2">
            <button
              className="inline-flex h-9 items-center gap-2 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-strong)] px-3 text-sm font-medium text-[var(--app-text)] transition hover:border-primary-400/50 hover:text-primary-500"
              onClick={() => setPath('')}
              title="返回根列表"
            >
              <Home size={16} />
              根
            </button>
            {data.parent && (
              <button
                className="inline-flex h-9 items-center gap-2 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-strong)] px-3 text-sm font-medium text-[var(--app-text)] transition hover:border-primary-400/50 hover:text-primary-500"
                onClick={() => setPath(data.parent || '')}
                title="返回上级目录"
              >
                <ChevronUp size={16} />
                上一级
              </button>
            )}
          </div>
          <span className="min-w-0 flex-1 truncate text-right font-mono text-xs text-[var(--app-muted)]" title={data.path}>
            {data.path}
          </span>
        </div>
      )}

      {!loading && data?.entries && data.entries.length > 0 && (
        <FileEntriesTable
          basePath={data.path || ''}
          entries={data.entries}
          recursive={recursive}
          selectedPath={fileOperations.selected?.path}
          selectedPaths={fileOperations.selectedPaths}
          onSelectAll={(checked) => fileOperations.setSelectedPaths(checked ? data.entries?.map((entry) => entry.path) ?? [] : [])}
          onToggleSelectedPath={fileOperations.toggleSelectedPath}
          onEnter={enter}
          onChoose={fileOperations.choose}
        />
      )}

      {!loading && data?.entries && data.entries.length === 0 && <p className="text-ink-50">空目录。</p>}
    </div>
  )
}
