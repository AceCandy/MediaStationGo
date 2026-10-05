# 官方离线接口验证（2026-10-05）

## 已验证

目标：《凡人百世书》第三季第81集，source_id `7655633797097999385`，video_id `7655636095014538302`。
原始官方App 7.3.9.32，在隔离redroid Android14 ARM native bridge中，无用户登录，能播放并只下载目标一集。第一次本轮下载文件20,347,168字节，与前次成品大小一致。前次成品H.264/AAC、1280×720、86.098005秒，完整解码通过；既有成品保留在工作区外。App初次清晰度菜单360/480/540/720P，没有看到1080P；不能证明服务器没有其他规格。

## 请求构建的直接观察

只读观察官方App Retrofit请求构建；原始URL、设备信息和正文限临时目录。正文gzip压缩，应从字节解压，不能直接按UTF-8转换（遇到空字节可能截断）。
打开官方下载选择页可触发批量模型预取，不能把批量模型请求中的20个视频ID视为20集下载。UI仅勾选81，确认开始下载（1）。
接口构建地址：`https://reading.snssdk.com/novel/player/multi_video_model/default/v1`。
业务正文：`dr_scene=default`，`mixed_video_id_map={"1004":[目标视频ID及预取的相邻ID]}`。
`biz_param`包括：`caller_scene=download`，`video_platform=1024`，`need_all_video_definition=true`；`detail_page_version=0`；`disable_digg_stat`、`disable_video_relate_book`、`need_mp4_align`、`use_os_player`、`use_server_dns`均false。
项目现有单集接口`/novel/player/video_model/v1/`使用`content_type=1`和`video_platform=3`，见`internal/hongguo/download_app.go`。
这是请求构建层URL，不保证是网络调度后的最终主机。

## Android外匿名复现

Go -overlay只替换临时测试副本/请求版本副本，没有修改生产代码、队列或业务数据。复用项目匿名客户端，只提交目标81（不预取其他集）。
- reading主机精确批量路径、项目api5主机同路径/末尾斜杠以及`multi_video_model/v1/`：请求返回空正文，未取得模型。
- 临时改App版本参数至73932/7.3.9.32：仍空正文。
- 临时gzip压缩请求并设置Content-Encoding：仍空正文。
- 原有单集接口带官方离线biz_param，content_type=1：得到约15KB模型，全部360/480/540/720P为ByteVC2。
- 单集content_type=1004和官方版本：同样只有ByteVC2。
这些测试仅验证请求返回情况，测试PASS不代表下载或解码成功。

## 未验证与限制

官方批量响应TypedInput为流包装`uk0.a$a`，没有安全的getBytes。只读观察响应构造仅能确认类型，尚未得到目标离线模型内容。后续观察正常读取的流尝试没有取得正文，App进入黑屏，停止该观察，不能把此轮作为有效响应证据。不消费、替换或伪造响应，不绕过授权/证书检查。
不能确认App下载是否来自批量响应直接H.264，不能声称App本地转码；设备登记、请求签名/公共参数或网络调度的哪一项造成空响应仍未定位。

## 结论

官方App离线下载是可用兜底证据；纯匿名接口尚未打通，不应把上述字段直接加入生产降级链并宣称可解决第81集。本轮只留下脱敏研究记录。下一步最小探索是独立核对官方批量接口的最终调度路径及正常解析后的模型；若依赖App运行态，则需另行讨论有限并发的Android兜底，不能冒充纯接口。
