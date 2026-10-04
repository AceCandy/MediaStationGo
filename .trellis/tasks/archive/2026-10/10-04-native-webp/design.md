# 设计

本地 dev.sh 利用 C 编译器的系统库搜索找到 libwebp.so.7/libwebpdemux.so.2，在忽略目录 .dev-cache/native-webp 生成无版本库名映射，仅供后端进程加载；Linux 明确使用外部链接生成动态 ELF。没有原生库时保留既有后备实现。

Docker 后端使用与 runtime 相同的 Alpine 3.23，并在目标架构上编译，安装 build-base 并启用 CGO/外部链接；Alpine runtime 安装 libwebp-dev，提供无版本链接名及运行库。保留多架构发布。ImageProxy 创建时只记录原生启用与否及库错误，不记录图片路径或请求。

风险：正式构建由交叉编译变为目标平台编译，非本机架构需要 buildx/QEMU；libwebp 编码版本可能造成同质量图片字节差异，但缓存仍有效。不改现有运行服务的管理方式。
