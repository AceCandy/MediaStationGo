// Run from web: node scripts/check-auth-session.mjs
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { URL, URLSearchParams } from 'node:url'
import console from 'node:console'
import vm from 'node:vm'
import ts from 'typescript'

const require = createRequire(import.meta.url)
const axios = require('axios')
const refreshes = []
const load = (path, mocks) => {
  const exports = {}
  vm.runInNewContext(ts.transpileModule(readFileSync(new URL(path, import.meta.url), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText, { exports, URLSearchParams, require: id => mocks[id] ?? require(id) })
  return exports
}
const { useAuthStore: store } = load('../src/stores/auth.ts', {
  'zustand/middleware': { persist: init => init },
  '../api/refresh': { refreshToken: rt => new Promise((resolve, reject) => refreshes.push({ rt, resolve, reject })) },
})
const login = (id = 'viewer') => store.getState().setSession(`access-${id}`, `refresh-${id}`, { id, tier: 'free' })
const pair = { token: 'rotated', refresh_token: 'rotated-refresh' }
for (const action of ['switch', 'logout', 'relogin']) {
  for (const success of [true, false]) {
    login()
    const pending = store.getState().tokenRefresh()
    if (action === 'logout') store.getState().logout()
    else login(action === 'switch' ? 'other' : 'viewer')
    const current = store.getState()
    if (success) refreshes.at(-1).resolve(pair)
    else refreshes.at(-1).reject(new Error('expired'))
    assert.equal(await pending, false)
    assert.equal(store.getState(), current, `${action}: stale refresh cannot mutate session`)
  }
}

const { api } = load('../src/api/client.ts', {
  '../stores/auth': { useAuthStore: store },
  '../stores/playProfile': { getActivePlayProfileId: () => '', getActivePlayProfilePinToken: () => '' },
})
const requests = []
api.defaults.adapter = config => new Promise((resolve, reject) => requests.push({ config, resolve, reject }))
const flush = async () => { for (let i = 0; i < 30; i++) await Promise.resolve() }
const unauthorized = row => row.reject(new axios.AxiosError('unauthorized', 'ERR_BAD_REQUEST', row.config, {}, { status: 401 }))
const outcome = promise => promise.then(value => ({ value }), error => ({ error }))

login('first')
const first = outcome(api.get('/first'))
const second = outcome(api.get('/second'))
await flush()
const before = refreshes.length
unauthorized(requests.at(-2))
unauthorized(requests.at(-1))
await flush()
assert.equal(refreshes.length, before + 1, 'same session shares one refresh')
const oldRefresh = refreshes.at(-1)
login('second')
const fresh = outcome(api.get('/fresh'))
await flush()
unauthorized(requests.at(-1))
await flush()
assert.equal(refreshes.length, before + 2, 'new session refresh does not wait on the old session')
oldRefresh.resolve(pair)
await flush()
assert.ok(axios.isCancel((await first).error))
assert.ok(axios.isCancel((await second).error))
assert.equal(store.getState().token, 'access-second')
refreshes.at(-1).resolve(pair)
await flush()
assert.equal(requests.at(-1).config.headers.Authorization, 'Bearer rotated')
const count = requests.length
unauthorized(requests.at(-1))
assert.equal((await fresh).error.response.status, 401)
assert.equal(requests.length, count, 'retried 401 is not refreshed again')

login('response-owner')
const stale = outcome(api.get('/stale'))
await flush()
const row = requests.at(-1)
login('replacement')
row.resolve({ status: 200, data: 'private', config: row.config })
assert.ok(axios.isCancel((await stale).error), 'old successful responses are canceled too')
assert.equal(store.getState().token, 'access-replacement')
login('expired')
const failed = outcome(api.get('/failed'))
await flush()
unauthorized(requests.at(-1))
await flush()
refreshes.at(-1).reject(new Error('expired'))
await failed
assert.equal(store.getState().token, null, 'current session failure logs out')

const libraryCalls = []
const { libraryAPI } = load('../src/api/library.ts', {
  './client': { api: { get: (url, config) => new Promise(resolve => libraryCalls.push({ url, config, resolve })) } },
  '../stores/auth': { useAuthStore: store },
  '../stores/playProfile': { getActivePlayProfileId: () => '' },
})
login('library')
const shared = libraryAPI.get('one')
assert.equal(libraryAPI.get('one'), shared, 'unowned requests still deduplicate')
const controller = new globalThis.AbortController()
const independent = libraryAPI.get('one', { signal: controller.signal })
assert.notEqual(independent, shared)
assert.equal(libraryCalls[1].config.signal, controller.signal)
controller.abort()
assert.equal(libraryCalls[0].config.signal, undefined, 'abort does not affect shared request')
login('library')
const nextSession = libraryAPI.get('one')
assert.notEqual(nextSession, shared, 'same-user relogin gets its own request')
for (const row of libraryCalls) row.resolve({ data: { id: 'one' } })
await Promise.all([shared, independent, nextSession])
console.log('Auth refresh isolation, shared refresh, stale responses and bounded retries passed')
