import type { FormEvent } from 'react'

import type { GenerateSTRMResult, STRMOutputPreset } from '../api/strm'
import type { Library } from '../types'
import {
  StrmGenerateForm,
  StrmGenerateHeader,
  StrmGenerateHint,
  StrmGenerateResultPanel,
} from './StrmGenerateSectionParts'

export type StrmGenerateSectionProps = {
  libraries: Library[]
  generateLibraryID: string
  baseURL: string
  outputDir: string
  outputPresets: STRMOutputPreset[]
  autoGenerate: boolean
  overwrite: boolean
  includeLocal: boolean
  preserveTree: boolean
  refreshLibrary: boolean
  scrapeAfter: boolean
  generating: boolean
  generateResult: GenerateSTRMResult | null
  onGenerate: (event: FormEvent) => void
  setGenerateLibraryID: (value: string) => void
  setBaseURL: (value: string) => void
  setOutputDir: (value: string) => void
  setAutoGenerate: (value: boolean) => void
  setOverwrite: (value: boolean) => void
  setIncludeLocal: (value: boolean) => void
  setPreserveTree: (value: boolean) => void
  setRefreshLibrary: (value: boolean) => void
  setScrapeAfter: (value: boolean) => void
}

export function StrmGenerateSection(props: StrmGenerateSectionProps) {
  return (
    <section className="glass-panel space-y-4">
      <StrmGenerateHeader />
      <StrmGenerateForm {...props} />
      <StrmGenerateHint />
      <StrmGenerateResultPanel result={props.generateResult} />
    </section>
  )
}
