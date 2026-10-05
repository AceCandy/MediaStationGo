// 在官方 App 内调用其离线 RPC；不修改方法、签名、响应或媒体内容。
'use strict';
const target = __VIDEO_ID__;
let attempts = 0;
const timer = setInterval(function () {
  Java.perform(function () {
    try {
      const app = Java.use('android.app.ActivityThread').currentApplication();
      if (!app) throw new Error('App not ready');
      Java.classFactory.loader = app.getClassLoader();
      const Gson = Java.use('com.google.gson.Gson');
      const Request = Java.use('seriessdk.com.dragon.read.saas.rpc.model.MGetVideoModelV2Request');
      const api = Java.use('bk8.a').a();
      const seconds = Java.use('java.util.concurrent.TimeUnit').SECONDS.value;
      const gson = Gson.$new();
      const body = JSON.stringify({
        dr_scene: 'default',
        mixed_video_id_map: { '1004': [target] },
        biz_param: {
          caller_scene: 'download', video_platform: 1024, need_all_video_definition: true,
          detail_page_version: 0, disable_digg_stat: false, disable_video_relate_book: false,
          need_mp4_align: false, use_os_player: false, use_server_dns: false
        }
      });
      const request = gson.fromJson.overload('java.lang.String', 'java.lang.Class').call(gson, body, Request.class);
      clearInterval(timer);
      send({ kind: 'ready' });
      try {
        const result = api.mGetVideoModelV2RxJava(request).timeout(45, seconds).blockingFirst();
        const response = JSON.parse(String(gson.toJson(result)));
        const entry = response.data && response.data[target];
        if (String(response.code) !== '0' || !entry || !entry.video_model) throw new Error('invalid model');
        const model = typeof entry.video_model === 'string' ? JSON.parse(entry.video_model) : entry.video_model;
        send({ kind: 'model', video_id: target, model: model });
      } catch (_) {
        send({ kind: 'error' });
      }
    } catch (_) {
      if (++attempts >= 45) {
        clearInterval(timer);
        send({ kind: 'error' });
      }
    }
  });
}, 1000);
