# 实现设计

- 共享 ImageVariants 由 ImageProxy 持有，MediaService 由 builder 注入同一实例。
- 现有 serveImageFile 保留为原始文件出口，变体包装器最终复用其 HTTP 语义。
- 无 CGO 的 gen2brain/webp v0.6.4 编码，x/image v0.24.0 缩放/BMP 解码；JPEG/PNG 使用标准库。
- 宽高上限 4096，源图片不超过 32 MiB / 32M 像素；保持比例、不放大。fillWidth/fillHeight 采用等比例覆盖裁剪。
- 默认质量 80；JPEG/PNG/WebP 显式格式。未知格式/不合法参数返回 400。
- cache key 由原图路径、大小、纳秒修改时间、标准化参数和算法版本哈希组成；下载落盘失败时以内容哈希为源标识。
- 编码并发 2，按缓存键合并同时进行的请求；原子写入，取消等待可退出。不清理缓存。
- 解码/编码失败回退原图并 no-store，避免把原图永久缓存为变体。
- Web 默认 maxWidth=640、quality=80、format=webp，背景图显式 maxWidth=1920；可 original:true。
- SW 保留尺寸/质量/格式的身份，仅在同规格间淘汰旧版本。
- JPEG/PNG 在缩放前应用 EXIF 方向；WebP 使用解码器 AutoRotate。GIF、APNG、动态 WebP 保留原图并 no-store，避免丢帧。
- 并发编码本身不可中断；排队可取消，编码后检查取消，不写缓存。
