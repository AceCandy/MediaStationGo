import { useEffect, useRef, useState } from 'react'
import { LoaderCircle, Save, Search, X } from 'lucide-react'
import toast from 'react-hot-toast'

import { mediaAPI, type ManualScrapeCandidate, type MediaMetadataUpdate } from '../api/library'
import type { Media } from '../types'
import { ManualScrapeCandidateList } from './ManualScrapeDialogSections'
import { ModalShell } from './ModalShell'

interface MetadataEditDialogProps {
  open: boolean
  media: Media | null
  mediaIds?: string[]
  mode?: 'media' | 'series'
  scopeLabel?: string
  onClose: () => void
  onSaved: (media: Media) => void | Promise<void>
}

export function MetadataEditDialog({
  open,
  media,
  mediaIds,
  mode = 'media',
  scopeLabel,
  onClose,
  onSaved,
}: MetadataEditDialogProps) {
  const [form, setForm] = useState({
    title: '',
    original_name: '',
    overview: '',
    year: '',
    release_date: '',
    rating: '',
    season_num: '',
    episode_num: '',
    tmdb_id: '',
    bangumi_id: '',
    douban_id: '',
    thetvdb_id: '',
    languages: '',
    countries: '',
    genres: '',
    nsfw: false,
  })
  const [saving, setSaving] = useState(false)
  const [doubanCandidates, setDoubanCandidates] = useState<ManualScrapeCandidate[]>([])
  const [doubanSearchOpen, setDoubanSearchOpen] = useState(false)
  const [doubanSearching, setDoubanSearching] = useState(false)
  const [doubanSearchError, setDoubanSearchError] = useState('')
  const doubanSearchRequest = useRef(0)

  useEffect(() => {
    if (!open || !media) return
    setForm({
      title: media.title || '',
      original_name: media.original_name || '',
      overview: media.overview || '',
      year: media.year > 0 ? String(media.year) : '',
      release_date: media.release_date || '',
      rating: media.rating > 0 ? String(media.rating) : '',
      season_num: media.season_num > 0 || media.episode_num > 0 ? String(media.season_num || 0) : '',
      episode_num: media.episode_num > 0 ? String(media.episode_num) : '',
      tmdb_id: media.tmdb_id > 0 ? String(media.tmdb_id) : '',
      bangumi_id: media.bangumi_id > 0 ? String(media.bangumi_id) : '',
      douban_id: media.douban_id || '',
      thetvdb_id: media.thetvdb_id || '',
      languages: media.languages || '',
      countries: media.countries || '',
      genres: media.genres || '',
      nsfw: !!media.nsfw,
    })
    setDoubanSearchOpen(false)
    setDoubanCandidates([])
    setDoubanSearchError('')
    setDoubanSearching(false)
    doubanSearchRequest.current += 1
  }, [open, media])

  if (!open || !media) return null

  const isSeries = mode === 'series'
  const targetIds = Array.from(new Set((!isSeries && mediaIds && mediaIds.length > 0 ? mediaIds : [media.id]).filter(Boolean)))
  const searchTitle = form.title.trim()
  const encodedSearchTitle = encodeURIComponent(searchTitle)

  const set = (key: keyof typeof form, value: string | boolean) => {
    setForm((prev) => ({ ...prev, [key]: value }))
  }
  const toNumber = (value: string) => {
    const trimmed = value.trim()
    if (!trimmed) return 0
    const parsed = Number(trimmed)
    return Number.isFinite(parsed) ? parsed : 0
  }
  const buildPayload = (): MediaMetadataUpdate => {
    const payload: MediaMetadataUpdate = {
      scope: isSeries ? 'series' : undefined,
      title: form.title,
      overview: form.overview,
      year: Math.trunc(toNumber(form.year)),
      release_date: form.release_date,
      rating: toNumber(form.rating),
      tmdb_id: Math.trunc(toNumber(form.tmdb_id)),
      bangumi_id: Math.trunc(toNumber(form.bangumi_id)),
      douban_id: form.douban_id,
      thetvdb_id: form.thetvdb_id,
      languages: form.languages,
      countries: form.countries,
      genres: form.genres,
      nsfw: form.nsfw,
    }
    if (!isSeries) {
      payload.original_name = form.original_name
      payload.season_num = Math.trunc(toNumber(form.season_num))
      payload.episode_num = Math.trunc(toNumber(form.episode_num))
    }
    return payload
  }
  const save = async () => {
    if (!form.title.trim()) {
      toast.error('标题不能为空')
      return
    }
    setSaving(true)
    try {
      const payload = buildPayload()
      let next: Media | null = null
      for (const id of targetIds) {
        const updated = await mediaAPI.updateMetadata(id, payload)
        if (!next || id === media.id) next = updated
      }
      if (!next) next = await mediaAPI.updateMetadata(media.id, payload)
      toast.success(isSeries ? '整剧元数据已保存，分集信息保持不变' : '元数据已保存')
      await onSaved(next)
      onClose()
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error || '保存失败'
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }
  const searchDouban = async () => {
    if (!searchTitle) return
    const requestID = ++doubanSearchRequest.current
    setDoubanSearchOpen(true)
    setDoubanSearching(true)
    setDoubanSearchError('')
    try {
      const items = await mediaAPI.manualScrapeSearch(media.id, { query: searchTitle, provider: 'douban' })
      if (requestID !== doubanSearchRequest.current) return
      setDoubanCandidates(items.filter((item) => !!item.douban_id))
    } catch (err: unknown) {
      if (requestID !== doubanSearchRequest.current) return
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error || '豆瓣搜索失败'
      setDoubanCandidates([])
      setDoubanSearchError(msg)
    } finally {
      if (requestID === doubanSearchRequest.current) setDoubanSearching(false)
    }
  }
  const closeDoubanSearch = () => {
    doubanSearchRequest.current += 1
    setDoubanSearchOpen(false)
    setDoubanSearching(false)
  }
  const selectDouban = (item: ManualScrapeCandidate) => {
    if (!item.douban_id) return
    set('douban_id', item.douban_id)
    closeDoubanSearch()
  }

  return (
    <ModalShell maxWidth="max-w-5xl" className="flex max-h-[88vh] flex-col" ariaLabel={isSeries ? '编辑整剧元数据' : '编辑元数据'}>
      <div className="modal-header">
        <div>
          <h2 className="font-display text-xl font-bold text-gray-900">
            {isSeries ? '编辑整剧元数据' : '编辑元数据'}
          </h2>
          <p className="mt-1 text-xs text-gray-500">{scopeLabel || '用于手动修正自采集或无法自动匹配的媒体。'}</p>
        </div>
        <button onClick={onClose} className="icon-btn" aria-label="关闭">
          <X size={16} />
        </button>
      </div>
        <div className="grid flex-1 gap-4 overflow-y-auto p-5 md:grid-cols-2">
          <Field label="标题" value={form.title} onChange={(value) => set('title', value)} />
          {!isSeries && <Field label="原名 / 单集名" value={form.original_name} onChange={(value) => set('original_name', value)} />}
          <Field label="年份" value={form.year} onChange={(value) => set('year', value)} inputMode="numeric" />
          <Field label="上映日期" value={form.release_date} onChange={(value) => set('release_date', value)} type="date" />
          <Field label="评分" value={form.rating} onChange={(value) => set('rating', value)} inputMode="decimal" />
          {!isSeries && <Field label="季" value={form.season_num} onChange={(value) => set('season_num', value)} inputMode="numeric" />}
          {!isSeries && <Field label="集" value={form.episode_num} onChange={(value) => set('episode_num', value)} inputMode="numeric" />}
          <Field label="TMDb ID" value={form.tmdb_id} onChange={(value) => set('tmdb_id', value)} inputMode="numeric" searchSite="TMDb" searchHref={searchTitle ? `https://www.themoviedb.org/search?query=${encodedSearchTitle}` : ''} />
          <Field label="Bangumi ID" value={form.bangumi_id} onChange={(value) => set('bangumi_id', value)} inputMode="numeric" searchSite="Bangumi" searchHref={searchTitle ? `https://bgm.tv/subject_search/${encodedSearchTitle}?cat=all` : ''} />
          <Field label="豆瓣 ID" value={form.douban_id} onChange={(value) => set('douban_id', value)} searchSite="豆瓣" onSearch={searchTitle ? searchDouban : undefined} />
          <Field label="TheTVDB ID" value={form.thetvdb_id} onChange={(value) => set('thetvdb_id', value)} searchSite="TheTVDB" searchHref={searchTitle ? `https://thetvdb.com/search?query=${encodedSearchTitle}` : ''} />
          <Field label="语言" value={form.languages} onChange={(value) => set('languages', value)} placeholder="zh,en" />
          <Field label="国家/地区" value={form.countries} onChange={(value) => set('countries', value)} placeholder="CN,JP,US" />
          <Field label="类型" value={form.genres} onChange={(value) => set('genres', value)} placeholder="剧情,动画" />
          <label className="flex h-11 items-center gap-2 rounded-xl border border-gray-200 px-3 text-sm font-semibold text-gray-700">
            <input
              type="checkbox"
              checked={form.nsfw}
              onChange={(event) => set('nsfw', event.target.checked)}
              className="h-4 w-4 rounded border-gray-300 text-brand-600"
            />
            成人内容
          </label>
          <label className="md:col-span-2">
            <span className="mb-1 block text-xs font-bold text-gray-500">简介</span>
            <textarea
              value={form.overview}
              onChange={(event) => set('overview', event.target.value)}
              rows={5}
              className="input-field px-3 py-2 font-semibold"
            />
          </label>
        </div>
      <div className="modal-footer">
        <button onClick={onClose} className="btn-outline px-4 py-2 shadow-none">取消</button>
        <button onClick={save} disabled={saving} className="btn-primary px-5 py-2">
          <Save size={16} />
          保存
        </button>
      </div>
      {doubanSearchOpen && (
        <ModalShell onClose={closeDoubanSearch} maxWidth="max-w-3xl" className="flex max-h-[80vh] flex-col" zIndex={100} ariaLabel="选择豆瓣条目">
          <div className="modal-header">
            <div>
              <h3 className="font-display text-lg font-bold text-gray-900">选择豆瓣条目</h3>
              <p className="mt-1 text-xs text-gray-500">搜索：{searchTitle}</p>
            </div>
            <button type="button" onClick={closeDoubanSearch} className="icon-btn" aria-label="关闭豆瓣候选">
              <X size={16} />
            </button>
          </div>
          <div className="flex-1 overflow-y-auto p-5">
            {doubanSearching ? (
              <div className="flex min-h-56 items-center justify-center gap-2 text-sm text-gray-500">
                <LoaderCircle size={18} className="animate-spin" />
                正在搜索豆瓣
              </div>
            ) : doubanSearchError ? (
              <div className="flex min-h-56 items-center justify-center rounded-xl border border-dashed border-gray-200 text-sm text-gray-500">
                {doubanSearchError}
              </div>
            ) : doubanCandidates.length === 0 ? (
              <div className="flex min-h-56 items-center justify-center rounded-xl border border-dashed border-gray-200 text-sm text-gray-500">
                没有找到豆瓣条目
              </div>
            ) : (
              <ManualScrapeCandidateList items={doubanCandidates} applyingKey="" onApply={selectDouban} />
            )}
          </div>
        </ModalShell>
      )}
    </ModalShell>
  )
}

function Field({
  label,
  value,
  onChange,
  placeholder,
  inputMode,
  type = 'text',
  searchSite,
  searchHref,
  onSearch,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  inputMode?: 'numeric' | 'decimal'
  type?: 'text' | 'date'
  searchSite?: string
  searchHref?: string
  onSearch?: () => void
}) {
  const canSearch = !!(searchHref || onSearch)
  return (
    <div className="relative">
      <label>
        <span className="mb-1 block text-xs font-bold text-gray-500">{label}</span>
        <input
          value={value}
          type={type}
          onChange={(event) => onChange(event.target.value)}
          placeholder={placeholder}
          inputMode={inputMode}
          className={`input-field h-11 px-3 py-2 font-semibold ${searchSite ? 'pr-12' : ''}`}
        />
      </label>
      {searchSite && (
        <button
          type="button"
          disabled={!canSearch}
          onClick={() => onSearch ? onSearch() : window.open(searchHref, '_blank', 'noopener,noreferrer')}
          aria-label={canSearch ? `在 ${searchSite} 搜索当前标题` : `在 ${searchSite} 搜索，请先填写标题`}
          title={canSearch ? `在 ${searchSite} 搜索当前标题` : '请先填写标题'}
          className="icon-btn absolute bottom-1 right-1 disabled:cursor-not-allowed disabled:opacity-40"
        >
          <Search size={16} aria-hidden="true" />
        </button>
      )}
    </div>
  )
}
