// Run from web: node scripts/check-image-url.mjs
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { URL, URLSearchParams } from 'node:url'
import console from 'node:console'
import vm from 'node:vm'
import ts from 'typescript'

const require = createRequire(import.meta.url)
let token = 'first-session'
const exports = {}
vm.runInNewContext(ts.transpileModule(readFileSync(new URL('../src/api/client.ts', import.meta.url), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText, {
  exports,
  URLSearchParams,
  require: id => id === '../stores/auth'
    ? { useAuthStore: { getState: () => ({ token }) } }
    : id === '../stores/playProfile'
      ? { getActivePlayProfileId: () => '', getActivePlayProfilePinToken: () => '' }
      : require(id),
})
const { imageURL, streamURL } = exports
const variant = 'maxWidth=640&quality=80&format=webp'
for (const path of ['/api/libraries/library/cover', '/api/artwork/asset', '/api/catalogs/hongguo/artwork/asset', '/api/img']) {
  const expected = `${path}?v=cover-version`
  assert.equal(imageURL(`${expected}&token=old&api_key=old&apiKey=old&ApiKey=old`), `${expected}&${variant}`)
  const before = imageURL(expected)
  token = 'refreshed-session'
  assert.equal(imageURL(expected), before)
}
assert.equal(imageURL(), '')
assert.equal(imageURL('https://images.example/poster.jpg'), `/api/img?url=https%3A%2F%2Fimages.example%2Fposter.jpg&${variant}`)
assert.equal(imageURL('/api/artwork/asset', 'r1', { refreshCache: true, retryFailed: true }), `/api/artwork/asset?v=r1&retry=1&refresh=1&${variant}`)
assert.equal(imageURL('/api/artwork/asset?MaxWidth=100&format=png&v=old', 'new', { maxWidth: 1920 }), '/api/artwork/asset?v=new&maxWidth=1920&quality=80&format=webp')
assert.equal(imageURL('/api/artwork/asset?maxWidth=100&quality=80&format=webp', undefined, { original: true }), '/api/artwork/asset')
assert.match(streamURL('media'), /token=refreshed-session/)
const sw = { URL, self: { addEventListener() {} } }
vm.createContext(sw)
vm.runInContext(readFileSync(new URL('../public/artwork-cache-sw.js', import.meta.url), 'utf8'), sw)
const identity = query => sw.artworkIdentity(new URL(`https://example.test/api/img?url=poster&${query}`))
assert.equal(identity('maxWidth=640&v=1'), identity('v=2&maxWidth=640&refresh=1'))
assert.notEqual(identity('maxWidth=640'), identity('maxWidth=1920'))
assert.notEqual(identity('quality=80'), identity('quality=90'))
assert.notEqual(identity('format=webp'), identity('format=png'))
console.log('Image URL, variant cache identity and playback compatibility checks passed')
