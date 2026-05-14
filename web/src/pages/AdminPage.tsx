import { FormEvent, useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { Trash2 } from 'lucide-react'

import { adminAPI } from '../api/admin'
import { libraryAPI } from '../api/library'
import type { Library, User } from '../types'

// Single-page admin console. We split the surface into three panels:
//   • Libraries  — create / delete / scan
//   • Users      — list / delete (cannot delete self)
//   • Settings   — generic key/value store viewer
//
// Each panel manages its own loading / refresh state.
export function AdminPage() {
  const [tab, setTab] = useState<'library' | 'users' | 'settings'>('library')
  return (
    <div className="space-y-6">
      <h1 className="font-display text-3xl font-bold text-white">管理后台</h1>
      <div className="flex gap-2 border-b border-white/10">
        {(['library', 'users', 'settings'] as const).map((k) => (
          <button
            key={k}
            onClick={() => setTab(k)}
            className={
              'border-b-2 px-4 py-2 text-sm transition ' +
              (tab === k
                ? 'border-primary-400 text-primary-400'
                : 'border-transparent text-slate-400 hover:text-white')
            }
          >
            {k === 'library' ? '媒体库' : k === 'users' ? '用户' : '设置'}
          </button>
        ))}
      </div>

      {tab === 'library' && <LibraryPanel />}
      {tab === 'users' && <UsersPanel />}
      {tab === 'settings' && <SettingsPanel />}
    </div>
  )
}

function LibraryPanel() {
  const [libs, setLibs] = useState<Library[]>([])
  const [name, setName] = useState('')
  const [path, setPath] = useState('')
  const [type, setType] = useState('movie')

  const refresh = () => libraryAPI.list().then(setLibs)
  useEffect(() => {
    refresh().catch(() => undefined)
  }, [])

  const handleCreate = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await libraryAPI.create(name, path, type)
      toast.success('媒体库已创建')
      setName('')
      setPath('')
      await refresh()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '创建失败'
      toast.error(msg)
    }
  }

  return (
    <div className="space-y-6">
      <form onSubmit={handleCreate} className="glass-panel grid gap-3 md:grid-cols-4">
        <input
          required
          className="input-base"
          placeholder="名称"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <input
          required
          className="input-base md:col-span-2"
          placeholder="服务器路径,如 /media/movies"
          value={path}
          onChange={(e) => setPath(e.target.value)}
        />
        <select className="input-base" value={type} onChange={(e) => setType(e.target.value)}>
          <option value="movie">电影</option>
          <option value="tv">电视剧</option>
          <option value="anime">动漫</option>
          <option value="music">音乐</option>
        </select>
        <button type="submit" className="neon-button md:col-span-4">
          新建媒体库
        </button>
      </form>

      <div className="glass-panel">
        <table className="w-full text-left text-sm">
          <thead className="text-xs uppercase tracking-wider text-slate-500">
            <tr>
              <th className="py-2">名称</th>
              <th>路径</th>
              <th>类型</th>
              <th className="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            {libs.map((l) => (
              <tr key={l.id} className="border-t border-white/5">
                <td className="py-2 text-white">{l.name}</td>
                <td className="text-slate-300">{l.path}</td>
                <td className="text-slate-300">{l.type}</td>
                <td className="space-x-2 py-2 text-right">
                  <button
                    className="rounded border border-primary-400/40 px-2 py-1 text-xs text-primary-400 hover:bg-primary-400/10"
                    onClick={async () => {
                      const r = await libraryAPI.scan(l.id)
                      toast.success(`扫描完成,新增 ${r.added}`)
                    }}
                  >
                    扫描
                  </button>
                  <button
                    className="rounded border border-red-400/40 px-2 py-1 text-xs text-red-400 hover:bg-red-400/10"
                    onClick={async () => {
                      if (!confirm(`确定删除「${l.name}」?`)) return
                      await libraryAPI.remove(l.id)
                      toast.success('已删除')
                      await refresh()
                    }}
                  >
                    <Trash2 size={12} />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function UsersPanel() {
  const [users, setUsers] = useState<User[]>([])
  const refresh = () => adminAPI.listUsers().then(setUsers)
  useEffect(() => {
    refresh().catch(() => undefined)
  }, [])

  return (
    <div className="glass-panel">
      <table className="w-full text-left text-sm">
        <thead className="text-xs uppercase tracking-wider text-slate-500">
          <tr>
            <th className="py-2">用户名</th>
            <th>角色</th>
            <th>最近登录</th>
            <th className="text-right">操作</th>
          </tr>
        </thead>
        <tbody>
          {users.map((u) => (
            <tr key={u.id} className="border-t border-white/5">
              <td className="py-2 text-white">{u.username}</td>
              <td className="text-slate-300">{u.role}</td>
              <td className="text-slate-400">
                {u.last_login_at ? new Date(u.last_login_at).toLocaleString() : '从未登录'}
              </td>
              <td className="py-2 text-right">
                <button
                  className="rounded border border-red-400/40 px-2 py-1 text-xs text-red-400 hover:bg-red-400/10"
                  onClick={async () => {
                    if (!confirm(`确定删除「${u.username}」?`)) return
                    await adminAPI.deleteUser(u.id)
                    toast.success('已删除')
                    await refresh()
                  }}
                >
                  <Trash2 size={12} />
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function SettingsPanel() {
  const [items, setItems] = useState<{ key: string; value: string }[]>([])
  const [k, setK] = useState('')
  const [v, setV] = useState('')

  const refresh = () => adminAPI.listSettings().then(setItems)
  useEffect(() => {
    refresh().catch(() => undefined)
  }, [])

  const onSave = async (e: FormEvent) => {
    e.preventDefault()
    await adminAPI.updateSetting(k, v)
    toast.success('已保存')
    setK('')
    setV('')
    await refresh()
  }

  return (
    <div className="space-y-4">
      <form onSubmit={onSave} className="glass-panel grid gap-3 md:grid-cols-3">
        <input
          required
          className="input-base"
          placeholder="键 (如 tmdb_api_key)"
          value={k}
          onChange={(e) => setK(e.target.value)}
        />
        <input
          required
          className="input-base"
          placeholder="值"
          value={v}
          onChange={(e) => setV(e.target.value)}
        />
        <button type="submit" className="neon-button">
          保存
        </button>
      </form>
      <div className="glass-panel">
        <table className="w-full text-left text-sm">
          <thead className="text-xs uppercase tracking-wider text-slate-500">
            <tr>
              <th className="py-2">键</th>
              <th>值</th>
            </tr>
          </thead>
          <tbody>
            {items.map((it) => (
              <tr key={it.key} className="border-t border-white/5">
                <td className="py-2 text-white">{it.key}</td>
                <td className="text-slate-300">{it.value}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
