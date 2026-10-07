# 执行与验证
1. 修改 enqueue、作品摘要、绑定与电影整理入口；补各层回归。
2. 修改Web接口类型、下载空间及合成响应fixture，验证电影/剧集混排。
3. 隔离PostgreSQL 16运行黄果相关 repository/service/handler 测试（含race），保留搜索分页回归。
4. npm run lint、npm run build、Go vet、git diff --check；合成页面使用独立Vite，结束时关闭浏览器/服务/临时数据库。
5. 独立只读审查，点验结论，记录测试和未验证事项。
