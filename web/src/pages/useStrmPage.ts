import { useEffect, useState } from 'react'

import { libraryAPI } from '../api/library'
import type { Library } from '../types'
import { useStrmAttachForm } from './useStrmAttachForm'
import { useStrmGenerateForm } from './useStrmGenerateForm'
import { useStrmImportForm } from './useStrmImportForm'

export function useStrmPage() {
  const [libraries, setLibraries] = useState<Library[]>([])
  const generate = useStrmGenerateForm(libraries)
  const importForm = useStrmImportForm(libraries)
  const attach = useStrmAttachForm()

  useEffect(() => {
    libraryAPI.list().then(setLibraries).catch(() => undefined)
  }, [])

  return {
    attach,
    generate,
    importForm,
    libraries,
  }
}
