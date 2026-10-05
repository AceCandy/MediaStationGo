import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'

const target = '7655636095014538302'
const script = readFileSync(new URL('../internal/hongguo/download_android.js', import.meta.url), 'utf8').replace('__VIDEO_ID__', JSON.stringify(target))
const model = { video_id: 'v0different', video_list: [{ main_url: 'https://example.com/media', video_meta: { codec_type: 'h264', definition: '720p' } }] }
for (const code of [0, '0', 403]) {
  for (const matched of [true, false]) {
    const messages = []
    let tick, body, calls = 0
    const response = { code, data: { [matched ? target : '123']: { video_model: JSON.stringify(model) } } }
    const gson = {
      fromJson: { overload: () => ({ call: (_self, text) => JSON.parse(text) }) },
      toJson: value => JSON.stringify(value),
    }
    const api = {
      mGetVideoModelV2RxJava(request) {
        calls++
        body = request
        return { timeout(seconds) { assert.equal(seconds, 45); return this }, blockingFirst: () => response }
      },
    }
    const classes = {
      'android.app.ActivityThread': { currentApplication: () => ({ getClassLoader: () => ({}) }) },
      'com.google.gson.Gson': { $new: () => gson },
      'seriessdk.com.dragon.read.saas.rpc.model.MGetVideoModelV2Request': { class: {} },
      'bk8.a': { a: () => api },
      'java.util.concurrent.TimeUnit': { SECONDS: { value: {} } },
    }
    vm.runInNewContext(script, {
      Java: { perform: action => action(), classFactory: {}, use: name => { assert.ok(classes[name], name); return classes[name] } },
      send: message => messages.push(message),
      setInterval: action => { tick = action; return 1 },
      clearInterval: () => {},
    })
    tick()
    assert.equal(calls, 1)
    assert.deepEqual(JSON.parse(JSON.stringify(body.mixed_video_id_map)), { '1004': [target] })
    assert.equal(body.dr_scene, 'default')
    assert.equal(body.biz_param.caller_scene, 'download')
    assert.equal(body.biz_param.video_platform, 1024)
    assert.equal(body.biz_param.need_all_video_definition, true)
    const final = messages.at(-1)
    assert.equal(final.kind, (String(code) === '0' && matched) ? 'model' : 'error')
    if (final.kind === 'model') {
      assert.equal(final.video_id, target)
      assert.equal(final.model.video_id, 'v0different')
    } else {
      assert.deepEqual(JSON.parse(JSON.stringify(final)), { kind: 'error' })
    }
  }
}
console.log('官方离线 RPC 参数、字符串/数字状态码、目标 Map 身份与错误脱敏检查通过')
