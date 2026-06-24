import { useEffect, useState } from 'react'
import toast from 'react-hot-toast'

import { libraryAPI } from '../api/library'
import {
  cloudAPI,
  storageAPI,
  type CloudEntry,
  type CloudScanStatus,
  type StorageType,
} from '../api/storage_config'
import { confirmAction } from '../components/confirmAction'
import type { Library } from '../types'
import {
  TYPE_LABEL,
  cloudLibraryProvider,
  cloudMountDisplayPath,
} from './storageConfigModel'

type CloudMountResult = {
  already_mounted?: boolean
  estimate_message?: string
  library?: Library
  message?: string
  reason?: string
  skipped?: boolean
}

function apiErrorMessage(err: unknown, fallback: string): string {
  return (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? fallback
}

export function useCloudBrowser(type: StorageType) {
  const [stack, setStack] = useState<{ id: string; name: string }[]>([{ id: '', name: '根目录' }])
  const [items, setItems] = useState<CloudEntry[]>([])
  const [mounts, setMounts] = useState<Library[]>([])
  const [loading, setLoading] = useState(false)
  const [mounting, setMounting] = useState(false)
  const [batchMounting, setBatchMounting] = useState(false)
  const [scanBusy, setScanBusy] = useState(false)
  const [cancelBusy, setCancelBusy] = useState(false)
  const [scanStatuses, setScanStatuses] = useState<CloudScanStatus[]>([])
  const [mountMediaType, setMountMediaType] = useState('auto')
  const [error, setError] = useState('')

  const cur = stack[stack.length - 1]
  const load = async (dir: string) => {
    setLoading(true)
    setError('')
    try {
      const r = await cloudAPI.list(type, dir)
      setItems(r.items ?? [])
      if (r.error) setError(r.error)
    } catch (err: unknown) {
      setError(apiErrorMessage(err, '加载失败'))
      setItems([])
    } finally {
      setLoading(false)
    }
  }

  const loadMounts = async () => {
    const libs = await libraryAPI.list({ includeHidden: true })
    setMounts(libs.filter((lib) => cloudLibraryProvider(lib.path) === type))
  }

  const loadScanStatus = async () => {
    const r = await storageAPI.cloudScanStatus()
    setScanStatuses((r.items ?? []).filter((item) => !type || item.provider === type))
  }

  useEffect(() => {
    load(cur.id).catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stack.length, type])

  useEffect(() => {
    loadMounts().catch(() => undefined)
    loadScanStatus().catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [type])

  useEffect(() => {
    const timer = window.setInterval(() => {
      loadScanStatus().catch(() => undefined)
    }, 3000)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [type])

  const enter = (entry: CloudEntry) => setStack((current) => [...current, { id: entry.id, name: entry.name }])
  const goTo = (index: number) => setStack((current) => current.slice(0, index + 1))
  const goUp = () => setStack((current) => (current.length > 1 ? current.slice(0, -1) : current))
  const currentMountPath = () => cloudMountDisplayPath(type, stack)
  const childMountPath = (child: CloudEntry) => cloudMountDisplayPath(type, stack, child)
  const currentDir = () => stack[stack.length - 1]?.id ?? ''

  const handleMountResult = (res: unknown, label: string) => {
    const out = res as CloudMountResult
    if (out.skipped) {
      toast(`已跳过「${label}」：和已挂载目录重叠`)
      return 'skipped'
    }
    if (out.already_mounted) {
      toast(`「${label}」已经挂载，后台会刷新扫描并自动入库`)
      return 'mounted'
    }
    toast.success(`已挂载「${label}」，${out.message ?? '后台会递归扫描并自动加入媒体库'}。${out.estimate_message ?? ''}`)
    return 'mounted'
  }

  const doImport = async (entry: CloudEntry) => {
    const ref = type === 'cloud115' ? entry.pick_code || entry.id : entry.id
    try {
      await cloudAPI.import(type, ref, entry.name, entry.size)
      toast.success(`已导入「${entry.name}」,可在媒体库中 302 播放`)
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, '导入失败'))
    }
  }

  const normalizeFolderInput = (value: string | null) => {
    const name = (value ?? '').trim()
    if (!name) return ''
    if (name === '.' || name === '..' || /[\\/]/.test(name)) {
      toast.error('文件夹名称不能包含路径分隔符')
      return ''
    }
    return name
  }

  const createFolder = async () => {
    const name = normalizeFolderInput(window.prompt('新建文件夹名称') ?? '')
    if (!name) return
    setLoading(true)
    try {
      await cloudAPI.mkdir(type, currentDir(), name)
      toast.success(`已新建文件夹「${name}」`)
      await load(currentDir())
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, '新建文件夹失败'))
    } finally {
      setLoading(false)
    }
  }

  const renameFolder = async (entry: CloudEntry) => {
    if (!entry.is_dir) return
    const name = normalizeFolderInput(window.prompt('重命名文件夹', entry.name) ?? '')
    if (!name || name === entry.name) return
    setLoading(true)
    try {
      await cloudAPI.rename(type, entry.id, name)
      toast.success(`已重命名为「${name}」`)
      await load(currentDir())
      await loadMounts()
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, '重命名失败'))
    } finally {
      setLoading(false)
    }
  }

  const mountCurrent = async () => {
    setMounting(true)
    try {
      const label = TYPE_LABEL[type] ?? type
      const name = cur.id ? cur.name : label
      const res = await cloudAPI.mount(type, cur.id, name, mountMediaType, currentMountPath())
      handleMountResult(res, cur.name)
      await loadMounts()
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, '挂载失败'))
    } finally {
      setMounting(false)
    }
  }

  const mountVisibleDirectories = async () => {
    const dirs = items.filter((item) => item.is_dir)
    if (dirs.length === 0) {
      toast.error('当前目录下没有可挂载的子目录')
      return
    }
    setBatchMounting(true)
    let ok = 0
    let skipped = 0
    let failed = 0
    for (const dir of dirs) {
      try {
        const result = await cloudAPI.mount(type, dir.id, dir.name, 'auto', childMountPath(dir))
        const state = handleMountResult(result, dir.name)
        if (state === 'skipped') skipped += 1
        else ok += 1
      } catch {
        failed += 1
      }
    }
    if (failed > 0) {
      toast.error(`已挂载 ${ok} 个目录，跳过 ${skipped} 个重叠目录，失败 ${failed} 个`)
    } else {
      toast.success(`已挂载 ${ok} 个目录，跳过 ${skipped} 个重叠目录，后台会自动生成 302/STRM 播放入口`)
    }
    await loadMounts()
    setBatchMounting(false)
  }

  const removeMount = async (lib: Library) => {
    const ok = await confirmAction({
      title: '移除网盘挂载',
      message: `仅移除「${lib.name}」在本项目中的媒体库和媒体记录，不会删除网盘文件。`,
      confirmText: '移除',
    })
    if (!ok) return
    await libraryAPI.remove(lib.id)
    toast.success('已移除挂载')
    await loadMounts()
  }

  const scanAllCloudLibraries = async () => {
    setScanBusy(true)
    try {
      const r = await storageAPI.scanAllCloud()
      setScanStatuses(r.items ?? [])
      toast.success(r.message ?? '已开始扫描所有启用的网盘媒体库')
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, '启动扫描失败'))
    } finally {
      setScanBusy(false)
    }
  }

  const cancelCloudScans = async () => {
    setCancelBusy(true)
    try {
      const r = await storageAPI.cancelCloudScan('', type)
      toast.success(r.message ?? `已中断 ${r.cancelled} 个扫描任务`)
      await loadScanStatus()
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, '中断扫描失败'))
    } finally {
      setCancelBusy(false)
    }
  }

  return {
    batchMounting,
    cancelBusy,
    cancelCloudScans,
    createFolder,
    enter,
    error,
    goTo,
    goUp,
    hasDirectories: items.some((item) => item.is_dir),
    items,
    loading,
    mountCurrent,
    mountMediaType,
    mounting,
    mounts,
    mountVisibleDirectories,
    removeMount,
    renameFolder,
    scanAllCloudLibraries,
    scanBusy,
    scanStatuses,
    setMountMediaType,
    stack,
    doImport,
  }
}
