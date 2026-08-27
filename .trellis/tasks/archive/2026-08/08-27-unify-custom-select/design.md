# Design

## Boundary

行为缺口是原生 `<select>` 会调用操作系统/浏览器弹层，无法继承项目主题。行为实际位于各页面原生元素以及缺失的共享 Select 原语。

预计仅新增一个 `web/src/components` 共享组件，并修改当前包含原生 `<select>` 的 17 个文件；如现有 `.input-field` 无法直接覆盖触发器和菜单，再对 `web/src/index.css` 做最小样式补充。除此之外不改动页面结构和业务逻辑。

## Component Contract

- 接收字符串值、选项数组、`onChange`、`disabled`、可访问名称及现有布局类。
- 触发器显示当前选项文本，菜单使用项目主题变量渲染选项与选中标记。
- 组件内部统一处理开关、外部点击、Escape、方向键、Home/End、Enter/Space 和焦点。
- 表单需要 `name` 的调用点保留同名隐藏字段，以维持原有表单提交语义。

## Compatibility and Rollback

- 页面仍持有状态和业务回调，组件只替代展示与交互层。
- 不新增依赖，不改变接口；回滚可按共享组件和调用点整体撤销。

