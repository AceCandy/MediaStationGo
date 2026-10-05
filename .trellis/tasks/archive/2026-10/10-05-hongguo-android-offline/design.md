# Design

用户已批准此方案及实施。Android14 amd64 + ARM bridge 常驻，持久化 /data，ADB仅Compose内网开放。Go可选工具镜像包含ADB与固定版本Android取模型工具，直接在Android执行，不增加辅助服务容器。

按用户最新要求，普通三来源各一次共三次预算，可选Android为第四次且不参与优先级。运行、校验失败重排队与恢复共用同一预算。新增source字符串无需迁移。只改下载客户端、执行器、来源显示和部署资料。

控制器串行在官方 App 内调用离线批量 RPC，仅请求目标数字分集 ID；使用 App 自身的客户端和签名处理，不改写方法。返回 data[目标数字ID].video_model，复用parseDownloadAppMedia选择兼容最高质量。canonical VOD ID不能代替数字ID核对。有限时获取、清理远端进程与临时文件后释放串行锁。取消包括等待锁。

原 UI 面板自动化被实际重复启动时的 GL 纹理错误阻断；宿主与软件 GPU 均重现。将 App 启动后立即退到 Android 首页保持后台，不播放视频，直接调用经 APK 固定版本源码及活体验证的 bk8.a.a().mGetVideoModelV2RxJava。此路径不再依赖菜单坐标、播放或全局 Gson hooks。仅适用于固定 APK 版本。

真实Android原生取模型工具运行需先验证，不能将启动工具成功当成模型获取成功。私有ADB控制地址来自部署配置；媒体下载仍使用现有公网地址验证。stdout、模型、设备值和keys只在内存/临时目录中，错误只能公开固定类别。

默认镜像和下载配置不变；使用可选Compose override启用。去掉override/环境变量并重建默认镜像即可回滚，已经完成的下载文件不受影响。
