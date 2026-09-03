import { FormEvent, useEffect, useState } from 'react'
import toast from 'react-hot-toast'

import { libraryAPI } from '../api/library'
import type { Library, LibraryRoot } from '../types'
import { confirmAction } from '../components/confirmAction'
import { apiErrorMessage, createRootPayload, displayLibraryRootPath, emptyRootDraft, rootDraftKey, type RootDraft } from './adminLibraryPanelModel'

export function useAdminLibraryPanel() {
  const { libs, refresh } = useAdminLibraryList()
  const createForm = useCreateLibraryForm(refresh)
  const editableRoots = useEditableRootDrafts()
  const rootActions = useEditableLibraryRootActions(refresh, editableRoots)
  const libraryActions = useLibraryActions(refresh)

  return { libs, createForm, editableRoots, rootActions, libraryActions }
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

function useEditableRootDrafts() {
  const [rootDrafts, setRootDrafts] = useState<Record<string, RootDraft>>({})

  const editableRootDraft = (libraryID: string, root: LibraryRoot): RootDraft => {
    const key = rootDraftKey(libraryID, root.id)
    return rootDrafts[key] ?? {
      path: displayLibraryRootPath(root.path),
      enabled: root.enabled,
      sort_order: root.sort_order,
    }
  }

  const setEditableRootDraft = (libraryID: string, root: LibraryRoot, patch: Partial<RootDraft>) => {
    const key = rootDraftKey(libraryID, root.id)
    setRootDrafts((prev) => ({ ...prev, [key]: { ...editableRootDraft(libraryID, root), ...patch } }))
  }

  const clearEditableRootDraft = (libraryID: string, rootID: string) => {
    setRootDrafts((prev) => {
      const next = { ...prev }
      delete next[rootDraftKey(libraryID, rootID)]
      return next
    })
  }

  return { editableRootDraft, setEditableRootDraft, clearEditableRootDraft }
}

type EditableRootDrafts = ReturnType<typeof useEditableRootDrafts>

function useEditableLibraryRootActions(refresh: () => Promise<void>, drafts: EditableRootDrafts) {
  const saveLibraryRoot = async (libraryID: string, root: LibraryRoot) => {
    const draft = drafts.editableRootDraft(libraryID, root)
    if (!draft.path?.trim()) {
      toast.error('请填写路径')
      return
    }
    await libraryAPI.updateRoot(libraryID, root.id, {
      path: draft.path.trim(),
      enabled: draft.enabled,
      sort_order: draft.sort_order,
    })
    drafts.clearEditableRootDraft(libraryID, root.id)
    toast.success('路径已保存')
    await refresh()
  }

  const toggleLibraryRoot = async (libraryID: string, root: LibraryRoot) => {
    const enabled = !drafts.editableRootDraft(libraryID, root).enabled
    drafts.setEditableRootDraft(libraryID, root, { enabled })
    await libraryAPI.updateRoot(libraryID, root.id, { enabled })
    await refresh()
  }

  const removeLibraryRoot = async (library: Library, root: LibraryRoot) => {
    if (!(await confirmAction({ title: '删除媒体库路径', message: `确定删除「${displayLibraryRootPath(root.path)}」?`, confirmText: '删除' }))) return
    await libraryAPI.removeRoot(library.id, root.id)
    toast.success('路径已删除')
    await refresh()
  }

  return { saveLibraryRoot, toggleLibraryRoot, removeLibraryRoot }
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
