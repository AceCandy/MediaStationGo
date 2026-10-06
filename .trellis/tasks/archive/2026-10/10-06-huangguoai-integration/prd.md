# 黄果 AI 独立站源完整接入

## Goal
在 MediaStationGo 独立接入黄果 AI：发现与三榜单、资料同步、管理员下载、扫描绑定、Web/Emby 播放及用户状态完整闭环。

## Confirmed scope
用户在 docs/huangguoai-design.md 完成多轮设计确认，于 2026-10-06 认可最终三项建议并明确“可以开干了”。新增独立来源表（14 基础 + 1 榜单），复用公共文件/任务/配置/权限；同一作品表涵盖两种类型。四分类 ai-huanlian/ai-mogai 固定 movie，ai-duanju/ai-manju 固定 series。默认 S01，无跨作品合集推断。保留源字符串 ID 与内部 UUID；电影 hga-work-UUID、剧集 hga-group-sourceID、季 hga-season-workUUID、集 hga-episode-episodeUUID。文件标签 [huangguoai-sourceID]。榜单 hot/recommend/potential。下载完成后通过整理/扫描入库，播放既有文件；来源纳入成人可见性控制。

## Exclusions
不接入黄果视频、旧 CloudFront 或黄果吃瓜文章；不改红果协议、App/Android，不添加 metadata_items 替身，不提供远程源站在线播放/动态源站 STRM，不扩展人物/跨作品合集/付费/账号平台。

## Acceptance criteria
1. 分类分页/去重与三个排名顺序正确，资料与图片公共 DTO 不泄漏源媒体地址或凭据；独立失败恢复与任务取消关闭。
2. 源协议合成测试覆盖数字精度/页码/缺字段/部分分集/计数冲突，真实完整一集取流及解码成功后才能声称下载链路可用。
3. 15 张表幂等迁移且不转换旧源；作品/分集身份稳定；收藏/进度跨版本和删除恢复保留。
4. 手动下载支持取消/租约/独立校验/无覆盖发布，完成与入库分开，失败不发布试看/残缺文件。
5. 文件标签及 S01 坐标精确绑定；无 metadata_items 替身时，Movie/Series 的 Web 与 Emby 列表→详情→播放→进度闭环正常。
6. Search/Latest/Resume/NextUp/收藏/统计/图片遵守文件与用户权限；大样本计数/候选/分页 SQL 计划有界，旧源回归通过。
7. Go 定向/集成/竞态测试、Web lint/build、接口/必要浏览器检查通过，临时文件清理，调试服务关闭。

## Technical evidence and risks
完整设计和已核验源码锚点见 design.md 引用文档。目录接口/分集页与三个榜单可读；源站搜索和三榜单已核验，7833 首集/尾集及完结单集 541 的完整下载/解码通过；没有已验证镜像，不保证全站资源可得。未知技术不得以虚构协议代替。跨类型分类冲突及电影真实多分集未观察到，保存异常而不改变固定类型，不作为擅自新增功能依据。
