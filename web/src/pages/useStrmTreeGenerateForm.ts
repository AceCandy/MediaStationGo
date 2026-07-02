import type { ChangeEvent, FormEvent } from 'react'
import { useState } from 'react'
import toast from 'react-hot-toast'

import { strmAPI, type GenerateSTRMResult } from '../api/strm'
import { currentOrigin } from './strmPageModel'
import { apiErrorMessage, isHTTPURL } from './strmPageUtils'

export function useStrmTreeGenerateForm() {
  const [provider, setProvider] = useState('openlist')
  const [sourceRoot, setSourceRoot] = useState('')
  const [outputPrefix, setOutputPrefix] = useState('')
  const [baseURL, setBaseURL] = useState(currentOrigin())
  const [outputDir, setOutputDir] = useState('data/strm/tree')
  const [treeText, setTreeText] = useState('')
  const [pathsText, setPathsText] = useState('')
  const [overwrite, setOverwrite] = useState(false)
  const [cleanup, setCleanup] = useState(false)
  const [runningMode, setRunningMode] = useState<'generate' | 'preview' | null>(null)
  const [result, setResult] = useState<GenerateSTRMResult | null>(null)

  const onImportTreeFile = async (event: ChangeEvent<HTMLInputElement>) => {
    const input = event.currentTarget
    const file = input.files?.[0]
    if (!file) return
    try {
      const text = await file.text()
      if (!text.trim()) {
        toast.error('目录树文件为空')
        return
      }
      setTreeText(text)
      toast.success(`已导入 ${file.name}`)
    } catch {
      toast.error('目录树文件读取失败')
    } finally {
      input.value = ''
    }
  }

  const runGenerate = async (dryRun: boolean) => {
    const trimmedBaseURL = baseURL.trim()
    const paths = parsePathList(pathsText)
    if (!treeText.trim() && paths.length === 0) {
      toast.error('请粘贴目录树或路径列表')
      return
    }
    if (trimmedBaseURL && !isHTTPURL(trimmedBaseURL)) {
      toast.error('域名必须以 http:// 或 https:// 开头')
      return
    }
    setRunningMode(dryRun ? 'preview' : 'generate')
    try {
      const next = await strmAPI.generateFromTree({
        provider,
        source_root: sourceRoot.trim() || undefined,
        output_prefix: outputPrefix.trim() || undefined,
        base_url: trimmedBaseURL.replace(/\/+$/, '') || undefined,
        output_dir: outputDir.trim(),
        tree_text: treeText.trim() || undefined,
        paths: paths.length > 0 ? paths : undefined,
        overwrite,
        cleanup,
        dry_run: dryRun,
      })
      setResult(next)
      if (dryRun) {
        toast.success(`预检完成：将处理 ${next.previewed ?? 0} 个视频 · 跳过 ${next.skipped}`)
      } else {
        toast.success(`生成完成：新增 ${next.generated} · 更新 ${next.updated} · 跳过 ${next.skipped}`)
      }
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, dryRun ? '目录树预检失败' : '目录树生成失败'))
    } finally {
      setRunningMode(null)
    }
  }

  const onGenerate = async (event: FormEvent) => {
    event.preventDefault()
    await runGenerate(false)
  }

  const onPreview = async () => {
    await runGenerate(true)
  }

  return {
    baseURL,
    cleanup,
    generating: runningMode !== null,
    onImportTreeFile,
    onGenerate,
    onPreview,
    outputDir,
    outputPrefix,
    overwrite,
    pathsText,
    provider,
    result,
    runningMode,
    setBaseURL,
    setCleanup,
    setOutputDir,
    setOutputPrefix,
    setOverwrite,
    setPathsText,
    setProvider,
    setSourceRoot,
    setTreeText,
    sourceRoot,
    treeText,
  }
}

function parsePathList(raw: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .forEach((line) => {
      if (line.startsWith('#')) return
      if (seen.has(line.toLowerCase())) return
      seen.add(line.toLowerCase())
      out.push(line)
    })
  return out
}
