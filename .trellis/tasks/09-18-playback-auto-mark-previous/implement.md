# 执行与验证

1. 实现系统设置子导航与默认关闭的播放选项；保留旧路由。
2. 实现普通资料及红果按季补标仓储操作，接入两条播放保存路径。
3. 添加真实 PostgreSQL 回归，覆盖关闭/未完成/完成、范围隔离、已看保留、事件及回滚。
4. 运行针对性 Go 测试、Web lint/build、git diff --check；浏览器检查标签、持久化、主题与 390/768/1024/1440 宽度。
5. 独立只读复核，修复确认的问题；更新播放规范，关闭调试服务并清理临时产物。

风险：客户端是否实际回报完成位置仍依赖外部播放器，部署设备实播无法由模拟回报代替。

## 验证结果

- 临时独立 PostgreSQL 15 / UTF8 数据库执行通过：TestPlaybackAutoMarkPreviousEpisodes（Web/Emby）、TestHongGuoAutoMarkPreviousEpisodes、TestPlaybackProgressRules、TestPlaybackProgressRecordingThreshold、TestEmbySeriesAndSeasonPlayedState、TestEmbyPlayedHierarchyScopeAndRollback、TestHongGuoEmbyPlayableIdentityAndUserState。
- Web npm run lint、npm run build、check-settings-tabs.mjs、git diff --check 通过。浏览器覆盖开关默认关闭、保存请求、读取已存配置、全部旧路径、设置和识别词草稿、非管理员拒绝、侧栏激活、390/768/1023/1024/1440 宽度与深浅主题；人工检查手机深色及桌面浅色截图。
- 独立后端复核无明确问题；前端首轮发现布局按路径重挂载导致草稿丢失，浏览器先复现再修正，二次独立复核通过。
- 扩展旧测试三项失败，在未改动的 107c896 临时工作树中均复现：TestEmbyPlaybackInfoProbesAllLocalSTRMVersions 引用已删除 duration_sec；TestEmbyMetadataVersionsShareUserStateAndKeepSourceIDs 的 Query callback 未捕捉 Scan 控制查询；TestPlaybackProgressRecordsOneEventPerSession 夹具缺少 media_probe_metadata。未扩展修改这些旧测试。
- 未验证：实际播放器设备回报、部署环境实播、全仓测试。Web 保存浏览器检查使用模拟 API；真实配置存取与补标逻辑由 PostgreSQL 回归覆盖。
- 实现与验证完成后，用户要求提交并归档；其余并行工作区变更保持原样，不纳入此任务提交。
