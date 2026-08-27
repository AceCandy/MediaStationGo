# Implementation Plan

1. 新增共享 Select 组件，覆盖现有调用所需的最小契约和键盘交互。
2. 逐个替换 17 个文件中的 29 个原生 `<select>`，保持原值、选项和回调。
3. 用 `rg` 确认产品代码无残留原生 `<select>`，人工核对各调用点类型与表单语义。
4. 执行 Web lint、build、`git diff --check`，再做一次独立代码复核。

## Risk and Rollback Points

- 风险集中在动态选项、可选空值、禁用态和表单 `name` 语义；替换时逐调用点核对。
- 若共享组件契约无法覆盖某一现有行为，保留该行为优先，不扩大业务改动。
