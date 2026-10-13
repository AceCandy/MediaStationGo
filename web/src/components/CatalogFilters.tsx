export type CatalogCategory = { value: string; label: string }

export function CatalogFilters({ label, options, value, onChange }: { label: string; options: readonly CatalogCategory[]; value: string; onChange: (value: string) => void }) {
  return <nav aria-label={label} className="tab-list max-w-full flex-wrap">{options.map(option => <button key={option.value} type="button" className="tab-item" aria-pressed={value === option.value} onClick={() => onChange(option.value)}>{option.label}</button>)}</nav>
}
