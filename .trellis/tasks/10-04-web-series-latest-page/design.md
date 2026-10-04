# 设计

只替换librarySeriesWorkPage。scoped物化可见作品及筛选、作品时间/归属。未知归属通过一次受限库文件扫描求资格，空unknown候选跳过文件；已知归属已表达本接口直接整剧/季/集文件资格，不再查全库日期。计数/分页共用works。仅为page展开整剧/季/集，按metadata ID先限定窄文件输入（OFFSET 0），再过滤库权限，避免统计滞后时逐分集重复扫库；关联后按旧代表排序计算Count/VersionCount。页面按作品最新入库时间、ID倒序。无schema/cache/global planner设置。

预计修改repository方法、分页回归与性能断言，以及两份列表契约。新排序授权来自用户本轮，其他路径原排序保持。验证新的时间口径，不沿用旧排序作为结果oracle。
