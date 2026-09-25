# 实施与验证

1. 最终方案获用户确认后 start；刷新 backend 红果、数据库及 Emby API catalog 规范，读取即将修改的真实代码与调用方。
2. 复用现有集成测试基础，在隔离数据库增加最小回归：库映射、多个作品/合集分页、隐藏库、播放状态及 MIN/MAX 时间、有无 NFO 的 Latest 一致性。先确认原实现能复现缺陷；不写真实数据库。
3. 修改共享库类型映射，落实红果作品层候选分页与 Latest 无 Count；保留不适用请求的原路径。
4. 同步 API catalog，gofmt 仅格式化本次 Go 文件。
5. 运行针对性 Go 测试，再运行相关 service/repository 回归；命令按实际新增测试名选择，例如 go test ./internal/service -run 'HongGuo|Emby.*Latest|Emby.*Library' -count=1。确认数据库测试没有因缺少环境变量而跳过。
6. 按 API catalog 规范运行前端 lint/build；独立复核 diff、过滤顺序、排序、总数与页内详情范围。git diff --check。
7. 不自动部署或重启真实服务。报告已验证与未验证内容；真实客户端刷新及生产耗时留待新版运行后确认。关闭本次自行启动的测试服务，清理调试产物。

风险文件：internal/service/emby_hongguo.go、internal/service/emby_system.go 及作品查询相关代码。优先复用现有函数；若需改变全局行为或数据库结构，停止并重新确认范围。
