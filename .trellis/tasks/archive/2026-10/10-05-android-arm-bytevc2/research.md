# Android ARM / ByteVC2 实验结果

## 验证结论

本机 Linux x86_64 / Ryzen 7 6800H 可以通过 redroid 原生桥执行 Android ARM64 JNI。公开官方播放器 SDK 的 ARM 库可加载、播放器可创建；实际播放在授权检查处失败，未得到任何解码帧。不能据此宣称 ByteVC2 已可播放或转码，也不能断言官方 SDK 绝对没有 ByteVC2 支持。

## 环境与证据

- 镜像：`redroid/redroid:12.0.0-latest`，digest `sha256:52332b2d74f337982d5ac281a8020ec297fb1ea05cbdcdaaa9c19a2065ae1adc`。
- 独立容器限制 4 CPU / 4 GiB RAM / 1024 PID，ADB 仅绑定 `127.0.0.1:5557`，软件 GPU；只绑定专用临时实验目录。
- `sys.boot_completed=1`。
- ABI：`x86_64,arm64-v8a,x86,armeabi-v7a,armeabi`。
- native bridge：`libnb.so`；镜像包含 `libndk_translation.so`。
- APK 只打包 `arm64-v8a` native 库，JNI `ARM64_PING=42`。
- 官方 `ttsdk-ttffmpeg/ttmp/ttlicense2/ttvcbasekit` 版本 `1.52.1.13`；BoringSSL 依赖 `2.0.2-16k-no-x86-SP`。SDK 来源为字节公开 Volcengine Maven 仓库。
- `avcodec_version=3758436`，H.264 解码器注册查询成功；`libbytevc2dec/bytevc2/bvc2/libvvcdec/vvc` 按名称查询均未发现。播放器创建前、后均如此；该查询不能覆盖所有自定义 C++ 解码路径。
- `vcbasekit/ByteVC1_dec/ttlicense2/ttmplayer` 均加载成功。
- 通过正常 `OnceConfig` / `TTPlayer` 构造创建播放器：`PLAYER_CREATED=true`。

## 实际播放控制实验

FFmpeg 自制 320×180、10 fps、5 秒 H.264 测试视频；播放器关闭硬解以检测软件路径，调用正常 setDataSource / prepare / start / takeScreenshot。

Android 日志：

```text
PLAYER_CREATED=true
PLAYER_ERROR=1,0 auth failed,authResult:403
EVENT=0,0,-30001
PREPARE=-30001
START=0
DECODE_COUNT=0 VC2_COUNT=0
```

没有实际 Bitmap 截图回调。`START=0` 只代表接口返回，不能当作开始解码的证据。公开下载 SDK 产物并不等于已获得使用许可；遇到授权失败后停止此路径，没有修改或绕过授权检查。

第 81 集没有在本阶段重新下载或送入播放器，因为基础控制视频已经在授权阶段被阻断。上一阶段已成功取得该集 720p ByteVC2 原始文件，但通用 FFmpeg 无法完整解码；这不是本次 Android 成功解码的证据。

## 下一步条件与限制

需要合法有效的 SDK 授权及对应初始化配置，先通过 H.264 实际帧控制验证，再测试第 81 集 ByteVC2。即使得到一帧，仍需验证整集解码、音视频同步、性能与可导出格式，才能讨论下载服务接入。本次不修改生产代码、队列、数据库或服务。

redroid 使用 privileged 容器，隔离不等于虚拟机安全边界；本次仅临时受控实验，不作为常驻生产方案。没有验证 ARM32、硬件解码、其他集、1080p 或批量转码。

## 可复核来源

- [redroid native bridge 文档](https://github.com/remote-android/redroid-doc#native-bridge-support)
- [Android 模拟器加速文档](https://developer.android.com/studio/run/emulator-acceleration)
- [官方 Maven 仓库](https://artifact.bytedance.com/repository/Volcengine/)

## 清理

清理和独立复核结果另记 check.md。只保留不含视频地址、密钥、设备身份的结论，删除测试媒体、APK、签名密钥、SDK 库与临时工具。

## 证据来源与重试方法

以上运行结果由本次终端工具调用及 Android logcat 直接读取并转录。播放器创建验证日志时间为 Android 容器 `10-05 03:39:32`，H.264 控制实验为 `10-05 03:39:56`。原始 APK、源码、工具及容器日志随实验清理，仓库仅保留脱敏转录，因此不能依靠现存文件重新执行原探针；后续重试需重建临时 APK，并提供合法授权。

关键只读验证命令为 `docker inspect <实验容器>`（资源和端口）、`docker exec <实验容器> getprop`（boot/ABI/bridge）、`docker exec <实验容器> logcat -d -s ARMProbe:I AndroidRuntime:E`（JNI/SDK/播放）。探针使用 `OnceConfig(null)` 和正常 `TTPlayer` 构造，`setIntOption(59,0)` 关闭硬解，经 `setDataSource`、`prepare`、`start`、`takeScreenshot` 检测。授权阻断后未继续测试第 81 集。
