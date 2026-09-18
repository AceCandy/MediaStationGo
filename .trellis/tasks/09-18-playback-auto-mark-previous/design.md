# 设计

保留现有 /admin/settings/* URL，在 SettingsPage 增加子导航。仅常规路由提供指向 /admin/settings 的侧栏“系统设置”入口，根路径继续重定向至常规。设置区域共用布局挂载标识，已访问的识别词面板隐藏而不卸载，避免子标签切换丢草稿。新增 watching 分组承载播放行为开关，沿用 settings 单键 API。

配置键 playback.auto_mark_previous_episodes，缺失默认 false。完成回报读取该键，不修改现有阈值。普通播放写入抽到两个现有调用方共享的服务方法；当前集历史、补标及当前集真实事件在同一事务提交。Emby 快路径不新增完整 MediaView/identifiers 读取。

普通资料以当前 Episode 的 parent_id 限定同季，用更小的 episode_num 查询可见文件，按 metadata_id 去重；已完成行冲突时不覆盖。红果按 source_id/episode_number，使用独立表和绑定，展示分组不改变身份。补标不产生事件。

仅关闭配置即可停止后续补标；既有补标通过原来的取消已看操作撤销，不设计自动回滚历史。无 schema 迁移。
