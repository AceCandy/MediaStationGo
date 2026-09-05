# 外部 ID 搜索与豆瓣状态区分

## Goal

降低手动查找外部媒体 ID 的成本，并让详情页的豆瓣关联状态无需悬停提示就能直观区分。

## Background

- 编辑元数据弹窗已有 TMDb、Bangumi、豆瓣、TheTVDB ID 字段，但没有按当前标题搜索对应站点的快捷入口。
- 详情页已有豆瓣来源徽标；未关联与已关联但数据不完整目前都使用黄色警告图标，视觉上难以区分。

## Requirements

- 外部 ID 字段可通过搜索按钮打开对应站点的搜索页面，搜索词使用当前表单标题。
- 搜索按钮覆盖 TMDb、Bangumi、豆瓣、TheTVDB 全部四个外部 ID 字段，无论字段当前是否已有 ID 都显示。
- 搜索使用浏览器外链，不新增后端搜索接口。
- 豆瓣未关联使用灰色断开链接图标；已关联但不完整或降级保留黄色感叹号；完整状态保留绿色完成图标。
- 保留已关联豆瓣时点击徽标跳转豆瓣详情页，以及管理员未关联时点击徽标打开元数据编辑弹窗的现有行为。
- 当前表单标题为空时不执行外部搜索。

## Acceptance Criteria

- [x] 用户可从元数据编辑弹窗按当前标题打开对应外部站点的搜索结果页。
- [x] 四个外部 ID 字段均提供搜索按钮，已有 ID 时按钮仍可使用。
- [x] 搜索页面在新标签页打开，不丢失当前表单内容。
- [x] 详情页无需查看 tooltip 即可区分豆瓣未关联与豆瓣信息不完整。
- [x] 未关联、不完整或降级、完整三种豆瓣状态分别呈现灰色断链、黄色感叹号、绿色完成图标。
- [x] 豆瓣已有跳转及管理员设置入口行为不回归。

## Out of Scope

- 不在应用内展示或解析外部站点搜索结果。
- 不新增外部搜索后端接口。

## Technical Notes

以下搜索地址已于 2026-09-05 使用浏览器按中文标题验证：

- TMDb：`https://www.themoviedb.org/search?query=<title>`
- Bangumi：`https://bgm.tv/subject_search/<title>?cat=all`
- 豆瓣：`https://search.douban.com/movie/subject_search?search_text=<title>&cat=1002`
- TheTVDB：`https://thetvdb.com/search?query=<title>`
