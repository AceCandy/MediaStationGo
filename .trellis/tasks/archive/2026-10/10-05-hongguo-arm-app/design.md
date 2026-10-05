# 方案

沿用已验证的 redroid Android 12 amd64 镜像和 ARM native bridge，独立容器只挂专用临时目录，限制 4 CPU / 4 GiB RAM / 4096 PID，ADB 仅本机 5557。安装官网二维码或其官方分发链提供的原始 APK，保留公开版本/摘要证据。使用 Android 自带 am/input/uiautomator/screencap 进行界面操作，不改变 App 或授权逻辑。出现需要用户登录的页面时暂停相关步骤；服务结束前停止。临时内容清理，仅保留脱敏研究与核验记录。

Android 12 对照发生 native bridge Arm64WriteToFpcr 断言，增加官方 Android 14 镜像对照验证。临时容器 lmkd 出现 epoll_wait errno=22 循环与误回收，保持 Docker 4 GiB 硬限制，必要时只在实验容器停用 lmkd；不修改宿主机内存回收设置。
