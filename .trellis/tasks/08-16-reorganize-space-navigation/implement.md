# 实施计划

1. 调整路由导航元数据
   - 增加 `files` scope，移除旧管理分组元数据。
   - 将指定七个页面归入文件空间，将剩余页面按明确顺序归入管理空间。
   - 将 `/admin/tasks/stats` 改为指向 `/admin/storage` 的 replace 重定向。
   - 验证：检查所有导航 route 的 scope、order、权限和 URL。

2. 简化侧栏导航投影
   - 生成“观看空间”“文件空间”“管理空间”三个一级组。
   - 删除管理空间二级分组构造和已失效的路径映射。
   - 验证：管理员与普通用户的可见项、活动高亮和自动展开符合 PRD。

3. 合并系统监控页面
   - 在规范页中组合存储概览和实时运行指标，并统一标题。
   - 保证两类数据独立加载、独立失败，保留 2 秒轮询和清理。
   - 删除本次变更造成的未使用页面和 import。
   - 验证：分别模拟正常、存储失败、监控失败和离开页面后的行为。

4. 独立复核与质量验证
   - 对照 PRD 逐项复核导航归属、顺序、权限、路由兼容和页面内容。
   - 在 `web` 目录运行 `npm run lint`、`npm run build`，并在仓库根目录运行 `git diff --check`。
   - 启动 Web 调试服务，使用 390x844、768x1024、1440x900 检查侧栏和系统监控页，确认无横向溢出、遮挡或新增控制台错误；验证后关闭服务。

## 重点文件

- `web/src/appRoutes.tsx`
- `web/src/components/layoutNavigation.ts`
- `web/src/pages/StoragePage.tsx`
- `web/src/pages/StatsPage.tsx`
- `web/src/pages/StatsPageSections.tsx`

## 回滚点

- 导航修改与页面合并均为前端代码变更，无数据迁移；可按上述文件恢复旧 route scope/group 和独立页面路由。
