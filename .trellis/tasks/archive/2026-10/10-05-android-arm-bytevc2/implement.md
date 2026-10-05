# Experiment steps

1. Inspect Docker/kernel capabilities and public redroid native bridge documentation.
2. Pull an official redroid image, record digest and boot an isolated container.
3. Verify Android completion and ABI/native bridge configuration; run a synthetic ARM library probe.
4. Probe official ByteVC2 decoder availability and attempt a decoded frame only if the decoder is present.
5. Independently inspect results and cleanup; record verified limitations, no production integration.

## 执行结果

1. 已验证独立 redroid 启动和资源边界。
2. 已验证 ARM64 JNI 执行、官方 SDK 库加载及完整播放器创建。
3. 补充 H.264 控制实验，prepare 在授权检查失败，未进入实际解码；停止 ByteVC2 帧实验。
4. 结果及未验证范围见 research.md；独立复核与清理见 check.md。
5. 完成实验阶段，无正式下载服务接入，无自动提交。
