export function DownloadProgress({ label, value, total }: { label: string; value: number; total: number }) {
  const percent = total > 0 ? Math.max(0, Math.min(100, value * 100 / total)) : undefined
  return <div role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent} aria-valuetext={percent === undefined ? '总大小未知' : undefined} className="h-0.5 overflow-hidden rounded-full bg-brand-500/10"><div className={`h-full rounded-full bg-gradient-to-r from-brand-500 to-sage-400 ${percent === undefined ? 'w-1/3 motion-safe:animate-pulse' : ''}`} style={percent === undefined ? undefined : { width: `${percent}%` }} /></div>
}
