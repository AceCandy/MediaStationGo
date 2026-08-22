import { FormEvent, useState } from 'react'
import { Plus } from 'lucide-react'

import { AdminLibraryCreateDialog } from './AdminLibraryPanelSections'
import { AdminLibraryGrid, LibraryDetailDialog } from './AdminLibraryTable'
import { useAdminLibraryPanel } from './useAdminLibraryPanel'

export function AdminLibraryPanel() {
  const { libs, createForm, editableRoots, rootActions, libraryActions } = useAdminLibraryPanel()
  const [createOpen, setCreateOpen] = useState(false)
  const [activeID, setActiveID] = useState<string | null>(null)
  const activeLib = activeID ? libs.find((l) => l.id === activeID) ?? null : null

  const handleCreate = async (e: FormEvent) => {
    if (await createForm.handleCreate(e)) setCreateOpen(false)
  }

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between gap-3">
        <p className="text-sm text-ink-50">共 {libs.length} 个媒体库</p>
        <button className="btn-primary" onClick={() => setCreateOpen(true)}>
          <Plus size={16} /> 新建媒体库
        </button>
      </div>

      <AdminLibraryGrid libs={libs} onSelect={(lib) => setActiveID(lib.id)} />

      {createOpen && (
        <AdminLibraryCreateDialog
          name={createForm.name}
          type={createForm.type}
          coverURL={createForm.coverURL}
          roots={createForm.roots}
          onNameChange={createForm.setName}
          onTypeChange={createForm.setType}
          onCoverURLChange={createForm.setCoverURL}
          onRootChange={createForm.updateRoot}
          onAddRoot={createForm.addRoot}
          onRemoveRoot={createForm.removeRoot}
          onSubmit={handleCreate}
          onClose={() => setCreateOpen(false)}
        />
      )}

      {activeLib && (
        <LibraryDetailDialog
          library={activeLib}
          editableRootDraft={editableRoots.editableRootDraft}
          onEditableRootChange={editableRoots.setEditableRootDraft}
          onSaveRoot={rootActions.saveLibraryRoot}
          onToggleRoot={rootActions.toggleLibraryRoot}
          onRemoveRoot={rootActions.removeLibraryRoot}
          onRemoveLibrary={libraryActions.removeLibrary}
          onAddLibraryRoot={libraryActions.addLibraryRoot}
          onEditLibraryCover={libraryActions.editLibraryCover}
          onClose={() => setActiveID(null)}
        />
      )}
    </div>
  )
}
