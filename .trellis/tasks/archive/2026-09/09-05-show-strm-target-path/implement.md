# 实施计划

1. 在 service 层基于可见 Media 复用 `readLocalSTRMTarget` 暴露实时目标读取能力。
2. 添加已认证 handler 与路由，只返回 `target`，并补充最小 handler/service 测试。
3. 添加前端 API 与随当前选中版本变化的独立请求状态，在本地路径下展示成功结果。
4. 运行相关 Go 测试、Web lint/build、`git diff --check`，再做独立复核。
