# 统一作品日期排序并修复播放器首页展示

## 目标与范围

- SenPlayer 在电影库发送包含 Movie、Series 的混合类型请求时，按所选库类型分流，电影库正常返回电影。
- 所有 HTTP NextUp 别名固定返回 Items 空数组、TotalRecordCount=0、StartIndex=0，保留认证和用户权限校验。
- 普通、NFO、红果、黄果 AI 的 Emby Movie/Series 作品列表，将 DateCreated 主排序统一映射到 DateLastContentAdded，使用持久化 latest_media_added_at；库内、全局、收藏和人物作品筛选保持一致。
- 保留 DateCreated 展示字段、各入口升降序默认值、稳定排序、权限、分页及总数规则；季集混合列表、Resume 和 Web 下一集行为保持原契约。

## 验收结果

- [x] 实际 SenPlayer 混合类型及多字段排序参数覆盖 movie/tv 库、准确计数与分页下界两种模式。
- [x] NextUp 大小写、用户路径与前缀别名固定空响应；SeriesId、非零偏移不改变空响应；Resume/Web 回归通过。
- [x] DateCreated,SortName 以 DateCreated 为首个支持字段，复用作品最新入库路径，新增文件可让旧作品排到前面。
- [x] 红果 4,000 个作品 / 40,000 个文件测试覆盖日期冲突、展示日期、页边界、两种计数模式，候选计划不读取媒体文件。
- [x] 60 万文件执行计划测试覆盖真实 DateCreated 首字段、库内分页、全局与收藏作品计划。
- [x] NFO、普通和黄果 AI 日期、权限、计数、季集层级回归通过；API 目录及规范同步。
- [x] Web lint/build、目录浏览器验证、独立只读复核、git diff --check 通过。

## 边界与剩余验证

本轮未执行生产迁移、部署或重启，也未推送远端。SenPlayer 是否隐藏空栏目及生产实际耗时需更新服务后实机验证。页内详情仍读取必要文件和播放状态，不能将候选 SQL 测试耗时等同于整个接口耗时。
