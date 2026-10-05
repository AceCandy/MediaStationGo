# 红果 Android 离线兜底

这是可选功能：现有 App、备用接口、官方网页各尝试一次，共三次失败后，增加一次 Android 离线来源。Go 获取官方 App 的目标分集模型，仍由原下载队列传输、校验和发布文件。来源显示为“Android 离线”；不是首选来源选项。

启用期间 Android 容器需常驻。`restart: unless-stopped` 自动恢复，命名数据卷保留 App 初始化状态。当前验证环境为 Linux amd64、redroid Android14、官方红果7.3.9.32，至少预留4CPU和4GiB内存额度以及 Android 镜像/数据磁盘空间。宿主内核需支持 binder；容器使用 privileged，应作为专用 Android 环境，不安装账户或其它业务 App。项目 Compose 模式的 ADB 仅在容器网络中提供；独立模拟器模式默认仅映射到宿主回环地址。一个 Android 容器只配一个项目实例，模型获取串行，取得名额后最多执行两分钟；排队仍受下载任务的总超时与取消控制。

## 部署

### 使用 `./dev.sh`

前后端继续运行在宿主机，无需构建项目 Docker 镜像。独立文件只定义 Android 服务，可以单独复制到模拟器宿主机；在文件所在目录执行：

```sh
docker compose -f docker-compose.hongguo-android-standalone.yml up -d --wait
```

默认将 ADB 映射到本机 `127.0.0.1:15555`，Android 数据保存到命名卷。若模拟器与 `dev.sh` 不在同一机器，将文件的端口映射改为 `模拟器宿主机内网IP:15555:5555`，并将下面所有 `127.0.0.1:15555` 替换为这个 IP 和宿主端口。ADB 可以控制特权 Android，只供可信内网客户端访问，勿暴露公网。

准备宿主工具（Linux amd64，需要 curl、unzip、xz）。以下目录已被 `.gitignore` 忽略；APK 和取模型工具版本与镜像一致：

```sh
mkdir -p .dev-cache/hongguo-android
curl -fsSL --retry 3 https://dl.google.com/android/repository/platform-tools-latest-linux.zip -o .dev-cache/hongguo-android/adb.zip
unzip -qo .dev-cache/hongguo-android/adb.zip -d .dev-cache/hongguo-android
curl -fsSL --retry 3 https://github.com/frida/frida/releases/download/16.7.19/frida-inject-16.7.19-android-x86_64.xz -o .dev-cache/hongguo-android/inject.xz
echo '5067656da28620d7016ff63b9149c75e9f08ffcc1fcf393d5adce9b4adf52026  .dev-cache/hongguo-android/inject.xz' | sha256sum -c -
xz -dc .dev-cache/hongguo-android/inject.xz > .dev-cache/hongguo-android/frida-inject-android
chmod 755 .dev-cache/hongguo-android/frida-inject-android
curl -fsSL --retry 3 https://lf9-apk.ugapk.cn/package/apk/novelread/12267_73932/novelread_seo_laxin_pc_android_v12267_73932_d587_1790246416.apk -o .dev-cache/hongguo-android/hongguo.apk
echo '1d668fcbd3f9547f173287a03b06dda0e34faad8228639b66b37faab4c5ec516  .dev-cache/hongguo-android/hongguo.apk' | sha256sum -c -
rm .dev-cache/hongguo-android/adb.zip .dev-cache/hongguo-android/inject.xz
```

每条命令成功后再继续；校验失败不要安装或执行。将以下三行加入现有 `.env`，保留其它配置：

```sh
MEDIASTATION_HONGGUO_ANDROID_ADB=127.0.0.1:15555
MEDIASTATION_HONGGUO_ANDROID_TOOLS=./.dev-cache/hongguo-android
PATH="$PWD/.dev-cache/hongguo-android/platform-tools:$PATH"
```

先完成一次 App 初始化；以下使用刚下载的宿主 adb，不需要停止或替换现有前后端：

```sh
export PATH="$PWD/.dev-cache/hongguo-android/platform-tools:$PATH"
adb connect 127.0.0.1:15555
adb -s 127.0.0.1:15555 install .dev-cache/hongguo-android/hongguo.apk
adb -s 127.0.0.1:15555 shell am start -n com.phoenix.read/com.dragon.read.pages.splash.SplashActivity
adb -s 127.0.0.1:15555 shell 'rm -f /data/local/tmp/setup-ui.xml; uiautomator dump /data/local/tmp/setup-ui.xml'
adb -s 127.0.0.1:15555 shell cat /data/local/tmp/setup-ui.xml
```

阅读弹窗文字和协议后自行确认：当前通知拒绝坐标为(360,770)，隐私“同意并继续”为(360,823)。对应命令为 `adb -s 127.0.0.1:15555 shell input tap 360 770` 和 `adb -s 127.0.0.1:15555 shell input tap 360 823`。确认首页加载后执行 `adb -s 127.0.0.1:15555 shell rm -f /data/local/tmp/setup-ui.xml`、`adb kill-server`。不要在下载运行时手动操作 Android。

最后退出旧的 `dev.sh` 并重新执行 `./dev.sh`，使后端加载新增环境变量，再在网页重试失败分集。安卓常驻，停止 `dev.sh` 不会停止安卓。关闭此功能时，注释 `.env` 的两个 `MEDIASTATION_HONGGUO_ANDROID_*` 配置并重启 `dev.sh`，再在模拟器宿主机执行 `docker compose -f docker-compose.hongguo-android-standalone.yml stop hongguo-android`；保留数据卷可下次继续使用，不执行 `down -v`。

### 项目也使用 Docker Compose

已有部署文件和路径继续使用，以下以 `docker-compose.yml` 为例，标准版/搜索版替换第一个 `-f` 文件即可。可选镜像从本项目代码构建，默认发布镜像没有 ADB 或取模型工具。

```sh
docker compose -f docker-compose.yml -f docker-compose.hongguo-android.yml build mediastation-go
docker compose -f docker-compose.yml -f docker-compose.hongguo-android.yml up -d
```

若本机默认构建器报 BuildKit 内容目录 `operation not permitted`，使用独立容器构建器；不需要修改宿主目录权限。以下方式已在本机验证，构建出的标签与覆盖文件一致：

```sh
docker buildx create --name hongguo-android-build --driver docker-container
docker buildx build --builder hongguo-android-build --platform linux/amd64 --target runtime-android -t mediastation-go:hongguo-android-local --load .
docker buildx rm hongguo-android-build
docker compose -f docker-compose.yml -f docker-compose.hongguo-android.yml up -d
```

首次启动必须初始化官方 App。下面操作在项目容器内完成，`adb` 使用其默认管理服务器；不要在下载运行时手动操作同一个 Android。

```sh
docker compose -f docker-compose.yml -f docker-compose.hongguo-android.yml exec mediastation-go adb connect hongguo-android:5555
docker compose -f docker-compose.yml -f docker-compose.hongguo-android.yml exec mediastation-go adb -s hongguo-android:5555 install /opt/hongguo/hongguo.apk
docker compose -f docker-compose.yml -f docker-compose.hongguo-android.yml exec mediastation-go adb -s hongguo-android:5555 shell am start -n com.phoenix.read/com.dragon.read.pages.splash.SplashActivity
```

用屏幕或 UI XML 检查初始弹窗；当前固定720×1280环境的通知“DON’T ALLOW”中心为(360,770)，隐私“同意并继续”中心为(360,823)。请先阅读 App 协议并自行确认，再执行对应点击。无需登录。本功能不会自动接受协议或输入账户。

```sh
# 查看弹窗文字与 bounds；dump失败时不要沿用旧文件。
docker compose -f docker-compose.yml -f docker-compose.hongguo-android.yml exec mediastation-go adb -s hongguo-android:5555 shell 'rm -f /data/local/tmp/setup-ui.xml; uiautomator dump /data/local/tmp/setup-ui.xml'
docker compose -f docker-compose.yml -f docker-compose.hongguo-android.yml exec mediastation-go adb -s hongguo-android:5555 shell cat /data/local/tmp/setup-ui.xml
# 以下仅在确认对应弹窗后使用。
docker compose -f docker-compose.yml -f docker-compose.hongguo-android.yml exec mediastation-go adb -s hongguo-android:5555 shell input tap 360 770
docker compose -f docker-compose.yml -f docker-compose.hongguo-android.yml exec mediastation-go adb -s hongguo-android:5555 shell input tap 360 823
# 等首页加载完成后清理初始化工具。
docker compose -f docker-compose.yml -f docker-compose.hongguo-android.yml exec mediastation-go adb -s hongguo-android:5555 shell rm -f /data/local/tmp/setup-ui.xml
docker compose -f docker-compose.yml -f docker-compose.hongguo-android.yml exec mediastation-go adb kill-server
```

之后在网页中重试原失败分集即可。App版本、屏幕分辨率和取模型工具均固定；升级 App 前需要重新验证自动化。普通镜像/未配置 `MEDIASTATION_HONGGUO_ANDROID_ADB` 只尝试普通三个来源各一次。配置地址采用 `host:port`，只指向专用可信 Android 控制端。

## 运行与回滚

下载时启动 App 后立即退到 Android 首页保持后台，在 App 内调用其原有离线批量 RPC 获取指定数字分集 ID 的模型，不播放视频或在 App 中下载整集。媒体地址/密钥只在内存，按返回 Map 的数字分集 ID 绑定；取消/超时清理取模型进程、脚本及专用 ADB 服务器。模型返回最高兼容质量；已验证《凡人百世书第三季》第81集是720P H.264、第82集是1080P HEVC，不能承诺所有集数均有1080P或所有作品可用。

容器/网络/App异常只消耗这一次兜底，记录固定错误类别；不会无限重启 Android。健康检查每十秒检查当前 `lmkd` 进程的近期错误；发现已确认的 `epoll_wait failed (errno=22)` 时，仅重启该内存管理服务并重新检查，不删除 App 数据、不关闭内存保护。容器显示 healthy 不代表每个模型都可获取。重试依然从现有首选来源开始。重启 Android 可保留数据卷；不要删除卷，除非明确要重新初始化。常驻资源占用、App UI变化及长期稳定性需要部署后持续观察。

关闭功能时先停止 Android，再仅用原 Compose 文件重建项目服务为默认镜像，去掉环境变量。保留数据卷可日后恢复，已完成文件不受影响：

```sh
docker compose -f docker-compose.yml -f docker-compose.hongguo-android.yml stop hongguo-android
docker compose -f docker-compose.yml up -d --force-recreate mediastation-go
```

更新独立 Compose 后，执行 `docker compose -f docker-compose.hongguo-android-standalone.yml up -d --wait` 应用健康检查；可能重建模拟器容器，保留原命名卷。后端代码更新后重启 `./dev.sh`，使排队计时修复生效。

## 显式验证

`TestDownloadAndroidLive` 需要单独已初始化的 Android，环境变量 `MEDIASTATION_TEST_HONGGUO_ANDROID_ADB` 和 `MEDIASTATION_TEST_HONGGUO_ANDROID_TOOLS`（含 `frida-inject-android` 的目录），PATH 中有 adb 和 ffmpeg。验证活跃超时后继续取模型、连续第81/82集身份与完整解码，临时媒体自动删除。数据库队列验证另需隔离 PostgreSQL 的 `MEDIASTATION_TEST_POSTGRES_DSN`；不应对生产队列运行测试。
