import type { FormEvent } from 'react'
import { useEffect, useState } from 'react'
import toast from 'react-hot-toast'

import { adminAPI } from '../api/admin'
import { strmAPI, type GenerateSTRMResult } from '../api/strm'
import type { Library } from '../types'
import {
  inferSTRMOutputRoot,
  preferredSTRMBaseURL,
  suggestedSTRMOutputDir,
} from './strmPageModel'
import { apiErrorMessage, isHTTPURL } from './strmPageUtils'

type OutputRootSource = 'explicit' | 'generated'

export function useStrmGenerateForm(libraries: Library[]) {
  const [generateLibraryID, setGenerateLibraryID] = useState('')
  const [baseURL, setBaseURL] = useState('')
  const [outputDir, setOutputDir] = useState('')
  const [outputRoot, setOutputRoot] = useState('')
  const [outputRootSource, setOutputRootSource] = useState<OutputRootSource>('explicit')
  const [outputScope, setOutputScope] = useState('')
  const [outputDirTouched, setOutputDirTouched] = useState(false)
  const [settingsLoaded, setSettingsLoaded] = useState(false)
  const [autoGenerate, setAutoGenerate] = useState(false)
  const [overwrite, setOverwrite] = useState(false)
  const [includeLocal, setIncludeLocal] = useState(true)
  const [preserveTree, setPreserveTree] = useState(false)
  const [refreshLibrary, setRefreshLibrary] = useState(true)
  const [scrapeAfter, setScrapeAfter] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [generateResult, setGenerateResult] = useState<GenerateSTRMResult | null>(null)

  useEffect(() => {
    adminAPI
      .listSettings()
      .then((rows) => {
        const settings = Object.fromEntries(rows.map((row) => [row.key, row.value]))
        const savedOutputDir = settings['strm.output_dir'] || ''

        setBaseURL(preferredSTRMBaseURL(settings['strm.base_url'] || settings['app.server_url'] || ''))
        setOutputDir(savedOutputDir)
        setOutputRoot(savedOutputDir)
        setOutputRootSource(settings['strm.output_scope'] === 'library' ? 'generated' : 'explicit')
        setOutputScope(settings['strm.output_scope'] || '')
        setOutputDirTouched(false)
        setAutoGenerate(settings['strm.auto_generate_enabled'] === 'true')
        setPreserveTree(settings['strm.preserve_tree'] === 'true')
        setSettingsLoaded(true)
      })
      .catch(() => setSettingsLoaded(true))
  }, [])

  useEffect(() => {
    if (!generateLibraryID && libraries[0]) setGenerateLibraryID(libraries[0].id)
  }, [libraries, generateLibraryID])

  useEffect(() => {
    if (!settingsLoaded || outputDirTouched || !generateLibraryID) return
    const root =
      outputScope === 'library' && outputRootSource === 'generated'
        ? inferSTRMOutputRoot(outputRoot, libraries)
        : outputRoot
    if (generateLibraryID === '*') {
      setOutputDir(root)
      return
    }
    const library = libraries.find((item) => item.id === generateLibraryID)
    if (library) setOutputDir(suggestedSTRMOutputDir(root, library))
  }, [settingsLoaded, outputDirTouched, outputRoot, outputRootSource, outputScope, generateLibraryID, libraries])

  const onOutputDirChange = (value: string) => {
    setOutputDir(value)
    setOutputRoot(value)
    setOutputRootSource('explicit')
    setOutputScope('')
    setOutputDirTouched(true)
  }

  const onGenerate = async (event: FormEvent) => {
    event.preventDefault()
    const trimmedBaseURL = baseURL.trim()
    if (!generateLibraryID || !trimmedBaseURL) return
    if (!isHTTPURL(trimmedBaseURL)) {
      toast.error('域名必须以 http:// 或 https:// 开头')
      return
    }

    setGenerating(true)
    const submittedOutputDir = outputDir.trim()
    const submittedOutputWasExplicit = outputDirTouched
    try {
      const result = await strmAPI.generate({
        library_id: generateLibraryID,
        base_url: trimmedBaseURL.replace(/\/+$/, ''),
        output_dir: submittedOutputDir,
        overwrite,
        enabled: autoGenerate,
        include_local: includeLocal,
        preserve_tree: preserveTree,
        refresh_library: refreshLibrary,
        scrape_after: refreshLibrary && scrapeAfter,
      })
      const nextOutputDir = result.output_dir || outputDir
      setGenerateResult(result)
      setOutputDir(nextOutputDir)
      setOutputRoot(submittedOutputWasExplicit ? submittedOutputDir : inferSTRMOutputRoot(nextOutputDir, libraries))
      setOutputRootSource(submittedOutputWasExplicit ? 'explicit' : 'generated')
      setOutputScope(generateLibraryID === '*' ? 'all' : 'library')
      setOutputDirTouched(false)
      toast.success(`生成完成：新增 ${result.generated} · 更新 ${result.updated} · 跳过 ${result.skipped}`)
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, '生成失败'))
    } finally {
      setGenerating(false)
    }
  }

  const setRefreshLibraryEnabled = (value: boolean) => {
    setRefreshLibrary(value)
    if (!value) setScrapeAfter(false)
  }

  return {
    autoGenerate,
    baseURL,
    generateLibraryID,
    generateResult,
    generating,
    onGenerate,
    outputDir,
    includeLocal,
    overwrite,
    preserveTree,
    refreshLibrary,
    scrapeAfter,
    setAutoGenerate,
    setBaseURL,
    setGenerateLibraryID,
    setIncludeLocal,
    setOutputDir: onOutputDirChange,
    setOverwrite,
    setPreserveTree,
    setRefreshLibrary: setRefreshLibraryEnabled,
    setScrapeAfter,
  }
}
