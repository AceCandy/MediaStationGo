# 完善媒体详情标签图标和发布日期

## Goal

让媒体详情页的概览标签更容易辨认，并展示已有的完整发布日期，而不是只显示年份。

## Background

- `MediaDetailMetadata.tsx` 当前只有评分和年份带图标；分辨率、时长、大小、封装格式均为纯文本。
- TMDB 和豆瓣入口当前使用 `TM`、`豆`文字代替品牌图标。
- 前端 `Media.release_date`、后端详情投影及元数据存储均已支持 `YYYY-MM-DD`；当前组件仅选择了 `media.year`。
- 项目已依赖 `lucide-react`，并已有本地品牌资源目录 `web/public/brand/`。

## Requirements

- 为分辨率、时长、文件大小和视频封装格式补充含义匹配的 Lucide 图标。
- 将 TMDB 文字缩写替换为 TMDB 官方提供的 SVG 品牌图标。
- 将豆瓣文字缩写替换为 Simple Icons 提供的豆瓣 SVG 品牌图标。
- 发布日期优先显示 `media.release_date`；没有完整日期时回退显示 `media.year`，两者都没有时不显示日期标签。
- 保留现有标签布局、主题变量、外链行为和 provider 缓存状态图标。

## Acceptance Criteria

- [x] 分辨率、时长、文件大小、封装格式标签均显示对应图标，图标不挤压或打乱换行布局。
- [x] TMDB 和豆瓣入口显示可辨认的品牌图标，不再显示 `TM`、`豆`文字缩写。
- [x] `release_date` 有值时显示到日（`YYYY-MM-DD`）。
- [x] `release_date` 为空且 `year > 0` 时仍显示年份；两者都无值时隐藏日期标签。
- [x] provider 完整、部分、缺失三种状态的图标、提示和无障碍文本保持不变。
- [x] 不新增前端依赖，不修改后端、数据库或抓取流程。

## Out of Scope

- 修改日期元数据的抓取、存储或编辑逻辑。
- 调整标签的业务字段、数值格式或 provider 状态规则。
- 重构详情页其他区域。

## Technical Notes

- TMDB 品牌资产来源：TMDB 官方 Logos & Attribution 页面中的 Primary short SVG。
- 豆瓣品牌资产来源：`simple-icons/simple-icons` 的 `icons/douban.svg`。
