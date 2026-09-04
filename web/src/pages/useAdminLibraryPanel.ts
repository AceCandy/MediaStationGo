import { FormEvent, useEffect, useState } from 'react'
import toast from 'react-hot-toast'

import { libraryAPI } from '../api/library'
import type { Library, LibraryRoot } from '../types'
import { confirmAction } from '../components/confirmAction'
import { apiErrorMessage, createRootPayload, displayLibraryRootPath, emptyRootDraft, type RootDraft } from './adminLibraryPanelModel'

export function useAdminLibraryPanel() {
  const { libs, refresh } = useAdminLibraryList()
  const createForm = useCreateLibraryForm(refresh)
  const rootActions = useLibraryRootActions(refresh)
  const libraryActions = useLibraryActions(refresh)

  return { libs, createForm, rootActions, libraryActions }
}

function useAdminLibraryList() {
  const [libs, setLibs] = useState<Library[]>([])
  const refresh = () => libraryAPI.list({ includeHidden: true }).then(setLibs)

  useEffect(() => {
    refresh().catch(() => undefined)
  }, [])

  return { libs, refresh }
}

function useCreateLibraryForm(refresh: () => Promise<void>) {
  const [name, setName] = useState('')
  const [roots, setRoots] = useState<RootDraft[]>([emptyRootDraft()])
  const [type, setType] = useState('movie')

  const handleCreate = async (e: FormEvent): Promise<boolean> => {
    e.preventDefault()
    try {
      const payload = createRootPayload(roots)
      if (payload.length === 0) {
        toast.error('请至少填写一个路径')
        return false
      }
      await libraryAPI.createWithRoots(name, type, payload)
      toast.success('媒体库已保存')
      setName('')
      setRoots([emptyRootDraft()])
      await refresh()
      return true
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, '创建失败'))
      return false
    }
  }

  const updateRoot = (index: number, patch: Partial<RootDraft>) => {
    setRoots((prev) => prev.map((root, i) => (i === index ? { ...root, ...patch } : root)))
  }

  return {
    name,
    type,
    roots,
    setName,
    setType,
    updateRoot,
    addRoot: () => setRoots((prev) => [...prev, emptyRootDraft()]),
    removeRoot: (index: number) => setRoots((prev) => (prev.length <= 1 ? prev : prev.filter((_, i) => i !== index))),
    handleCreate,
  }
}

function useLibraryRootActions(refresh: () => Promise<void>) {
  const toggleLibraryRoot = async (libraryID: string, root: LibraryRoot) => {
    const enabled = !root.enabled
    const action = enabled ? '启用' : '禁用'
    if (!(await confirmAction({
      title: `${action}媒体库路径`,
      message: `确定${action}「${displayLibraryRootPath(root.path)}」？`,
      confirmText: action,
      danger: !enabled,
    }))) return
    await libraryAPI.updateRoot(libraryID, root.id, { enabled })
    toast.success(`路径已${action}`)
    await refresh()
  }

  const removeLibraryRoot = async (library: Library, root: LibraryRoot) => {
    if (!(await confirmAction({ title: '删除媒体库路径', message: `确定删除「${displayLibraryRootPath(root.path)}」?`, confirmText: '删除' }))) return
    await libraryAPI.removeRoot(library.id, root.id)
    toast.success('路径已删除')
    await refresh()
  }

  return { toggleLibraryRoot, removeLibraryRoot }
}

function useLibraryActions(refresh: () => Promise<void>) {
  const removeLibrary = async (library: Library) => {
    if (!(await confirmAction({ title: '删除媒体库', message: `确定删除「${library.name}」?`, confirmText: '删除' }))) return
    await libraryAPI.remove(library.id)
    toast.success('已删除')
    await refresh()
  }

  const addLibraryRoot = async (library: Library, root: RootDraft): Promise<boolean> => {
    if (!root.path.trim()) {
      toast.error('请填写路径')
      return false
    }
    try {
      await libraryAPI.addRoot(library.id, { path: root.path.trim(), enabled: true })
      toast.success('来源目录已添加')
      await refresh()
      return true
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, '添加路径失败'))
      return false
    }
  }

  const uploadLibraryCover = async (library: Library, cover: File) => {
    try {
      await libraryAPI.uploadCover(library.id, cover)
      toast.success('媒体库封面已保存')
      await refresh()
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, '上传封面失败'))
    }
  }

  const clearLibraryCover = async (library: Library) => {
    try {
      await libraryAPI.clearCover(library.id)
      toast.success('媒体库封面已清除')
      await refresh()
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, '清除封面失败'))
    }
  }

  return { removeLibrary, addLibraryRoot, uploadLibraryCover, clearLibraryCover }
}
