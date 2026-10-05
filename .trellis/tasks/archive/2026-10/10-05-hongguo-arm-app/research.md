# 官方红果 App ARM 验证

## 安装来源

官网 https://hongguoduanju.com/ 下载二维码 → https://applink.novelquickapp.com/doRXv → 字节官方 APK 分发域名 lf9-apk.ugapk.cn。没有使用第三方修改包。二维码中的临时网页身份参数不入仓。

- 包名 `com.phoenix.read`，版本 `7.3.9.32`，versionCode `73932`，minSdk 21，targetSdk 35。
- APK 仅含 `arm64-v8a`，Android 安装结果 Success，primaryCpuAbi=arm64-v8a。
- APK 字节数：145240577。
- APK SHA-256：`1d668fcbd3f9547f173287a03b06dda0e34faad8228639b66b37faab4c5ec516`。
- 包内包含 `libbyteVC2dec.so`。文件存在只表明随包分发，不能单凭文件名证明已解码视频。

## Android 12 对照

镜像 digest `sha256:52332b2d74f337982d5ac281a8020ec297fb1ea05cbdcdaaa9c19a2065ae1adc`。

App 可进入首页，未要求登录。官网作品页已确认目标为《凡人百世书第三季》，使用官网二维码提供的 `dragon8662://videoDetail` 路由和目标作品/分集 ID 打开播放页，但页面出现黑屏与加载停滞，没有可验证视频画面，也未在界面成功确认第 81 集标题。

运行中读取到：

```text
F ndk_translation: vendor/unbundled_google/libs/ndk_translation/intrinsics/intrinsics_impl_x86_64.cc:86: CHECK failed: 524288 == 0
F om.phoenix.rea: runtime.cc:669] Runtime aborting...
native: /system/lib64/libndk_translation.so (ndk_translation::intrinsics::Arm64WriteToFpcr(unsigned long)+210)
```

另有插件 `libnslinker.so` JNI_OnLoad 返回 JNI_ERR。未修改 App、native bridge 或断言。可以确认有原生桥运行兼容故障；不能证明此断言由 ByteVC2 解码器触发，不能将黑屏单独归因为该编码。

最初 1024 PID 限制引起 pthread_create 失败，已提升至 4096。lmkd 反复 epoll_wait errno=22 且出现 LOW_MEMORY 退出记录，在实验容器停止 lmkd 后复测仍遇到上面的 native bridge 断言。保持 4 CPU、4 GiB Docker 硬内存限制，只绑定专用临时目录，未改变宿主或生产服务。

## Android 14 对照

镜像 amd64 digest `sha256:11d58a64bfbde2253d1cce81bff409ff58174980222d1bada232d9ef59181191`，Android 14，native bridge `libnb.so` / `libndk_translation.so`，同一原始 APK。

先尝试 ro.lmk.use_psi=false 时 lmkd 无法初始化，未完成启动；重建容器恢复默认 LMK 配置后 sys.boot_completed=1，安装、启动成功。资源限制 4 CPU / 4 GiB / 4096 PID，ADB 127.0.0.1:5557。Android 14 成功运行期间未重现上述 native bridge fatal 日志，不据此断言所有其他 App 或编码都兼容。

目标页显示“凡人百世书第三季”“第81集”，相隔时间的截图显示人物场景、字幕和进度条变化，证实实际画面播放。进程映射包含 libbyteVC2dec.so、libttffmpeg.so、libttmplayer.so；**加载库不等于证实当前播放选择了 ByteVC2**，本次没有采集线上播放的实际 codec 字段。首页推荐视频也显示实际画面。全过程未登录账户、未填写手机号/验证码；关闭了通知权限提示。

## 官方单集下载与成品

通过播放页右上角菜单 → 下载到本地 → 61–90 → 只选81，确认“开始下载（1）”后下载；“我的下载”显示该作品已下载1集、19.4MB。清晰度菜单为 360P / 480P / 540P / 720P，当前720P，没有看到1080P选项。

App 在 `Android/data/com.phoenix.read/files/ttvideo_offline/` 写入一个 .mdl 文件（另有节点配置文件，本次没有读取其中配置内容或密钥）。检查媒体本身：

```json
{"video":"h264","video_tag":"avc1","width":1280,"height":720,"audio":"aac","audio_tag":"mp4a","duration":86.098005,"bytes":20347168}
```

- .mdl 实际是普通 MP4 容器，不是根据后缀猜测。
- FFmpeg 不提供 decryption_key，不改文件，完整解码 video+audio，`-xerror` 下 exit=0，stderr为空。
- 从成品第15秒提取出有效1280×720画面。
- 下载选择与“我的下载”验证对应第81集，打开已下载条目仍显示第81集和相同作品名。
- SHA-256：`110a33a4ecf8aa9286a5bd518518466e499ad3533ee2ea8637aac072c04fcb42`。
- 成品复制到仓库外 `/vol1/1000/ssd/workai/hongguo-results/凡人百世书第三季-第81集-720p.mp4`，只更改文件名后缀，字节与缓存一致。此为用户目标视频交付物，保留；临时测试副本、截图、APK和环境全部清理。

完整解码命令：

```sh
ffmpeg -nostdin -hide_banner -loglevel error -xerror -protocol_whitelist file,pipe -i <成品.mp4> -map 0:v:0 -map 0:a:0 -f null -
```

## 结论与未验证范围

这台 x86_64 机器可通过 redroid Android14 + ARM bridge 运行官方红果 App，目标第81集可实际播放，并通过 App 官方下载入口得到可被常规 FFmpeg 完整解码的720P H.264/AAC文件。

这纠正了先前只能从已试接口拿到 ByteVC2 的范围：官方 App 下载路径确实能获得兼容成品；但没有跟踪下载请求或确认它如何选择/生成该成品，不能断言现有接口只需某个参数即可复现，也不能断言 App 做了本地转码。没有绕过SDK授权、解密保护或修改App。本次没有把该路径接入生产下载服务。

未验证1080P、其他集、批量自动化、长时间稳定性、音画同步主观体验（仅音视频全程解码通过）、直接API复现或实时播放具体编码。Privileged redroid 是临时实验，不等于推荐常驻生产方案。

## 证据来源

本次 Android 屏幕/UI dump、包管理器、/proc 进程库映射、logcat 及宿主 FFprobe/FFmpeg。临时截图覆盖目标标题、不同视频帧、720P菜单、仅选81和开始下载1集；由独立复核读取后清理。原始日志可能包含匿名设备信息，只保留上述脱敏错误转录。成品可供后续独立复核媒体元数据和完整解码。

## 外部资料

- https://github.com/remote-android/redroid-doc#native-bridge-support
- https://github.com/zhouziyang/libndk_translation （预编译库索引，未找到本次FPCR断言的明确修复说明）


## 清理结果

独立复核后关闭App及专用ADB服务器，停止并删除实验容器，删除本次Android12/14镜像和所有临时文件。复查5038/5557端口关闭、目标容器和镜像不存在。只保留上文指明的成品MP4及脱敏任务记录。工作树中的并行Emby改动未触碰。
