# 验收记录

## 已完成
- 定位16条失败，限定11条Android最后来源任务，排除5条分集变更保护。
- lmkd系统调用实测：epoll_pwait(3, ..., -1, -1, NULL, 8)返回EINVAL；ANR中AMS锁持有线程停在LmkdConnection.write/__sendmsg。仅重启lmkd后恢复阻塞等待。
- bridge两分钟预算移动到取得gate之后；synctest验证排队3分钟仍能执行，父context取消不释放其他请求的名额。
- 版本解析区分正确数字版本、错误数字版本、明确缺包及系统无有效输出；fake ADB验证包管理服务恢复后继续检查。真实Android缺包输出也核实为Unable to find package。
- 两份Compose检测当前lmkdPID近期精确errno22并仅重启该服务；日志读取失败返回不健康。健康/故障/启动未完成/缺PID/日志失败五状态模拟通过，真实健康状态通过。
- 实际部署独立Compose，容器健康、App73932和原初始化数据保留；docker inspect确认最终容器内脚本为单$ shell展开。
- hongguo全包race、相关Go vet、Node RPC mock、四Compose组合解析通过。
- 隔离Postgres16 service race：Android管线五子场景、独立校验恢复、取消后重试、各来源一次（三优先级）、Android最后一次通过（54.956s）。
- 新控制器真实Android队列第81集传输、全量音视频解码和发布通过（15.08s），测试schema/媒体自动清理。隔离PG容器已关闭删除。
- 独立只读复核bridge与Compose；修复健康检查logcat退出码遗漏，并复验故障分支。

## 生产恢复
经现有HongGuoDownloadService.Action逐条重试11个固定UUID，没有启动额外下载执行器、修改并发设置或关闭用户服务。现有full_verification=false保持不变，恢复脚本另行对每个发布文件检查散列/大小及完整音视频软件解码。当前dev.sh未重启，生产逐条恢复验证不代表旧进程已加载新排队代码。

最终11/11任务已completed，均为Android第4来源、720P；《仙缘错渡》5/6集为HEVC，其余为H.264。11/11发布文件大小/散列与检查点一致，完整音视频软件解码成功。首轮《星际航行录第一部》14集媒体请求超时，单独经同一Action入口重试后完成并解码；没有修改自动来源预算或添加HTTP重试。两轮都对5条排除记录做完整结构比对并保持不变。

最终数据库只有原5条failed，无queued/downloading/waiting_verify/verifying/publishing。Android healthy、无OOM、部署后lmkdPID18保持不变，无近期errno22；真实负载中未触发自动恢复。控制器没有App/inject残留进程；本地请求目录0。移除20:12故障留下且无活跃进程的旧remote请求目录后，远端请求目录0。生产成品保留，本地部署工具保留；本轮一次性恢复脚本和调研源码删除。

## 未验证/边界
- 未证明修复了redroid/AOSP上游事件计数缺陷；本次为精确故障检测与服务恢复。没有关闭PSI或内存保护。真实故障已手动恢复，自动故障分支目前通过模拟验证。
- 未做长期运行、所有作品集数、跨机器或云盘接入验收。
- 未运行全量service测试；既有WorkPagePlan查询性能问题不在本任务范围。
- 未修改排除的5条分集保护记录，不尝试整部重下。
