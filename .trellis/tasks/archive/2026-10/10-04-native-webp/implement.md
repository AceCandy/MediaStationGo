# 实施与验证

1. 修改本地启动、Docker构建和一次启动后端日志；补充本地依赖说明。
2. 原生库实际加载门禁、图片变体回归、race与nodynamic回归；镜像验证和冷/热隔离测量。
3. 独立只读审查，记录通过项目与未验证项，清理临时产物。

## 最终验证

- 原生门禁先红：无库名映射时外部链接的测试报 libwebp.so 不存在；应用本地映射后通过，验证实际库加载和 WebP 尺寸。
- 本地原生 image/artwork/cover/FFmpeg/FFprobe/cookie 相关 service、handler、middleware 测试通过；原生 TestImageVariant race 通过，CGO_ENABLED=0 nodynamic 后备回归通过。
- go vet 对 service、handler、middleware 通过；Web image URL/缓存身份/播放兼容脚本通过。bash -n 通过；使用隔离空配置及 go/npm stub 验证 dev.sh 在 CGO_ENABLED=0 的外部环境中为 Linux 后端设置 CGO=1、外部链接及真实库名映射，无真实服务启动。
- 最终 Dockerfile 的 amd64 完整正式镜像构建通过（含前端构建）；builder 与 runtime 均固定 Alpine 3.23。用同一 runtime 额外构建测试阶段，运行原生门禁及全部 TestImageVariant（10 个顶层测试）通过；正式二进制 ldd 通过，运行镜像存在两个无版本库链接。
- 3 张真实本地原图、maxHeight=720/quality=90，每张独立临时缓存测 3 次中位数：首次生成 103.67/121.72/265.00ms，热命中 0.19/0.16/0.43ms。此前同样原图后备实现首次 375.00/470.11/530.94ms。此为本地图片服务路径隔离测量，不含上游网络、真实客户端与并发排队。
- 两轮独立只读复核完成。手动命令的运行库前提已补充；第二轮称 runtime 不应安装开发包，此为误报：v0.6.4 必须解析无版本 .so 名，libwebp-dev 正是提供对应链接且自动包含运行库的必要依赖，镜像内门禁实测通过。
- diff 空白检查通过；新增日志只有后端类型及库加载错误，未引入图片路径、请求内容或凭据。临时源码/测量数据、Docker测试文件及日志清理；本地库名映射保留在已忽略的 .dev-cache，测试镜像删除。

未验证：arm64/QEMU 完整构建、Windows/macOS、真实客户端端到端耗时及数据库联调。本机 buildx 无 arm64 执行平台；现有发布 workflow 有 QEMU 初始化，但不将配置视为实测。现有开发服务未重启、镜像未发布，实际生效需按新 dev.sh 重启或部署新镜像。未修改质量/尺寸、缓存键、并发数或源图；没有清理原缓存。代码已提交，按用户要求归档。
