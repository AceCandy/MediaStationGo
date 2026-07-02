import { FolderTree, Loader2, Upload, X } from 'lucide-react'

import { currentOrigin } from './strmPageModel'
import { StrmGenerateResultPanel } from './StrmGenerateSectionParts'
import type { useStrmTreeGenerateForm } from './useStrmTreeGenerateForm'

type StrmTreeGenerateSectionProps = ReturnType<typeof useStrmTreeGenerateForm>

const outputPrefixPresets = [
  '电影/演唱会',
  '电影/纪录片',
  '电影/动画电影',
  '电影/华语电影',
  '电影/日韩电影',
  '电影/欧美电影',
  '电视剧/纪录片',
  '电视剧/儿童',
  '电视剧/综艺',
  '电视剧/国产剧',
  '电视剧/日韩剧',
  '电视剧/欧美剧',
  '动漫/日番',
  '动漫/国漫',
  '动漫/韩漫',
  '动漫/美漫',
  '动漫/其他',
  '成人',
]

export function StrmTreeGenerateSection({
  baseURL,
  batchLimit,
  cleanup,
  generating,
  onGenerate,
  onPreview,
  onImportTreeFile,
  outputDir,
  outputPrefix,
  overwrite,
  pathsText,
  provider,
  result,
  runningMode,
  setBaseURL,
  setBatchLimit,
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
}: StrmTreeGenerateSectionProps) {
  const batchLimitEnabled = Number.parseInt(batchLimit, 10) > 0

  return (
    <section className="glass-panel space-y-4">
      <div>
        <h2 className="font-display text-lg font-semibold text-ink-600">目录树生成 STRM</h2>
        <p className="text-sm text-ink-50">从网盘目录树或路径列表直接生成 .strm 文件。</p>
      </div>
      <form onSubmit={onGenerate} className="grid gap-3 md:grid-cols-4">
        <select className="input-base" value={provider} onChange={(e) => setProvider(e.target.value)}>
          <option value="openlist">OpenList</option>
          <option value="alist">Alist</option>
          <option value="cloud115">115</option>
          <option value="webdav">WebDAV</option>
          <option value="clouddrive2">CloudDrive2</option>
        </select>
        <input
          className="input-base"
          placeholder="/电视剧"
          value={sourceRoot}
          onChange={(e) => setSourceRoot(e.target.value)}
        />
        <input
          required
          className="input-base"
          placeholder="输出目录"
          value={outputDir}
          onChange={(e) => setOutputDir(e.target.value)}
        />
        <select
          className="input-base"
          value={outputPrefixPresets.includes(outputPrefix) ? outputPrefix : ''}
          onChange={(e) => setOutputPrefix(e.target.value)}
        >
          <option value="">选择输出分类</option>
          <optgroup label="电影">
            <option value="电影/演唱会">演唱会</option>
            <option value="电影/纪录片">纪录片</option>
            <option value="电影/动画电影">动画电影</option>
            <option value="电影/华语电影">华语电影</option>
            <option value="电影/日韩电影">日韩电影</option>
            <option value="电影/欧美电影">欧美电影</option>
          </optgroup>
          <optgroup label="电视剧">
            <option value="电视剧/纪录片">纪录片</option>
            <option value="电视剧/儿童">儿童</option>
            <option value="电视剧/综艺">综艺</option>
            <option value="电视剧/国产剧">国产剧</option>
            <option value="电视剧/日韩剧">日韩剧</option>
            <option value="电视剧/欧美剧">欧美剧</option>
          </optgroup>
          <optgroup label="动漫">
            <option value="动漫/日番">日番</option>
            <option value="动漫/国漫">国漫</option>
            <option value="动漫/韩漫">韩漫</option>
            <option value="动漫/美漫">美漫</option>
            <option value="动漫/其他">其他</option>
          </optgroup>
          <option value="成人">成人</option>
        </select>
        <input
          className="input-base"
          placeholder="输出分类，如 电影/欧美电影"
          value={outputPrefix}
          onChange={(e) => setOutputPrefix(e.target.value)}
        />
        <input
          className="input-base"
          inputMode="numeric"
          min={0}
          placeholder="每批数量"
          step={1}
          type="number"
          value={batchLimit}
          onChange={(e) => setBatchLimit(e.target.value)}
        />
        <input
          className="input-base md:col-span-3"
          placeholder="http://NAS-IP:18080 或 https://media.example.com"
          value={baseURL}
          onChange={(e) => setBaseURL(e.target.value)}
        />
        <button
          type="button"
          className="rounded-2xl border border-primary-400/40 px-3 py-2 text-sm text-brand-500 transition hover:bg-primary-400/10"
          onClick={() => setBaseURL(currentOrigin())}
        >
          使用当前访问地址
        </button>
        <textarea
          className="input-base min-h-44 md:col-span-4"
          placeholder={'电视剧\n├── 国产剧\n│   └── 南部档案\n│       └── Archives.S01E01.mkv'}
          value={treeText}
          onChange={(e) => setTreeText(e.target.value)}
        />
        <textarea
          className="input-base min-h-32 md:col-span-4"
          placeholder={'/电视剧/国产剧/南部档案/Season 01/Archives.S01E01.mkv\ncloud://openlist/电影/欧美电影/Dune.Part.Two.2024.mkv'}
          value={pathsText}
          onChange={(e) => setPathsText(e.target.value)}
        />
        <div className="flex flex-wrap items-center gap-2 md:col-span-4">
          <label className="inline-flex min-h-10 cursor-pointer items-center gap-2 rounded-2xl border border-gray-200 bg-white/70 px-3 py-2 text-sm font-medium text-ink-500 transition hover:border-primary-300 hover:text-brand-500">
            <Upload size={16} />
            导入文本
            <input className="sr-only" type="file" accept=".txt,.tree,text/plain" onChange={onImportTreeFile} />
          </label>
          <button
            type="button"
            className="inline-flex min-h-10 items-center gap-2 rounded-2xl border border-gray-200 bg-white/70 px-3 py-2 text-sm font-medium text-ink-500 transition hover:border-red-200 hover:text-red-500 disabled:cursor-not-allowed disabled:opacity-50"
            disabled={!treeText.trim() && !pathsText.trim()}
            onClick={() => {
              setTreeText('')
              setPathsText('')
            }}
          >
            <X size={16} />
            清空
          </button>
        </div>
        <label className="flex min-h-10 items-center gap-2 rounded-2xl border border-gray-200 bg-white/70 px-3 py-2 text-sm text-ink-50 md:col-span-4">
          <input type="checkbox" checked={overwrite} onChange={(e) => setOverwrite(e.target.checked)} />
          覆盖已存在
        </label>
        <label className="flex min-h-10 items-center gap-2 rounded-2xl border border-gray-200 bg-white/70 px-3 py-2 text-sm text-ink-50 md:col-span-4">
          <input
            type="checkbox"
            checked={!batchLimitEnabled && cleanup}
            disabled={batchLimitEnabled}
            onChange={(e) => setCleanup(e.target.checked)}
          />
          清理不在当前目录树中的旧 STRM
        </label>
        <button
          type="button"
          disabled={generating || (!treeText.trim() && !pathsText.trim()) || !outputDir.trim()}
          className="inline-flex min-h-10 items-center justify-center gap-2 rounded-2xl border border-primary-400/40 px-3 py-2 text-sm font-medium text-brand-500 transition hover:bg-primary-400/10 disabled:cursor-not-allowed disabled:opacity-50 md:col-span-2"
          onClick={onPreview}
        >
          {runningMode === 'preview' ? <Loader2 size={16} className="animate-spin" /> : <FolderTree size={16} />}
          {runningMode === 'preview' ? '预检中...' : '预检目录树'}
        </button>
        <button
          type="submit"
          disabled={generating || (!treeText.trim() && !pathsText.trim()) || !outputDir.trim()}
          className="neon-button md:col-span-2"
        >
          {runningMode === 'generate' ? <Loader2 size={16} className="animate-spin" /> : <FolderTree size={16} />}
          {runningMode === 'generate' ? '生成中...' : '按目录树生成'}
        </button>
      </form>
      <StrmGenerateResultPanel result={result} />
    </section>
  )
}
