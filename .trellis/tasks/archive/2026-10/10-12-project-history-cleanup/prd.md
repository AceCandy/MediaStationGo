# 全项目历史依赖清理

## 目标与授权
用户要求先提交归档已有工作，再实施全项目历史清理审计中的十二项。项目未上线，不保留旧版升级路径、旧URL或旧浏览器格式。现行资料中心、本地业务投影、播放和缓存能力保持。

## 范围与验收
1. API配置只有model.APIConfig和service.APIConfigService，保留加密、局部更新、豆瓣revision和当前管理UI；移除第二套model/repository/service/HTTP入口。
2. 启动只维护当前schema、索引、约束、触发器与必要运行投影，不保留旧表/字段/设置迁移、原图和缩略图迁移。现行图片读写只接受catalogs规范键；远程缓存、旁置缩略图、原图鉴权保持。
3. 定向清理已核验无数据、无业务依赖的series表；清理已结束的tmdb_center_migration/tmdb_snapshot_backfill执行记录。不得删除本地metadata、中心主体、人物、图片、媒体、用户状态或正常任务历史。
4. 移除旧库Path补建root的迁移；保留现行库创建、相对挂载路径映射及需要的主root投影。
5. 任务只按已持久化system过滤，移除nfo_scan/nfo_watch别名和专用旧入口，普通NFO扫描/监听保持。
6. 豆瓣中心仅movie/series，移除季集历史只读分支，保持电影/整剧校验与管理功能。
7. 删除旧页面路径和管理页tab兼容，发现页只读当前版本存储；保留现行默认跳转、URL规范化和权限。
8. 删除STRM /app/data重映射、未用docker-cli、GetEmbyUserID；保留通用路径映射、ffmpeg和Emby认证。
9. 更新当前图片目录、备份说明和相关规范；同步移除退役测试，保留现行业务回归。
10. 通过全包编译、受影响专项真实PostgreSQL测试、前端lint/build及独立只读复核。不启动/重启用户服务，不入仓core或凭据，不把当前来源标签legacy误判为旧存储。

## 明确边界
不删除扫描提示和DTO、不重写媒体身份、不合并来源人物表、不删除image proxy远程缓存、不退役版本化probe摘要回填、不开展额外性能/安全改造。
