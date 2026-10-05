# 第二轮离线模型调研（2026-10-05）

## 本轮关键结论

官方App离线批量响应直接返回目标第81集的H.264资源，不需要把已得到的ByteVC2在本机转码。
Android外复用项目媒体选择器与HTTP下载客户端，成功下载该H.264资源；FFmpeg `-xerror`完整视频/音频解码退出0。20347168字节，SHA-256 `110a33a4ecf8aa9286a5bd518518466e499ad3533ee2ea8637aac072c04fcb42`，与前次App成品逐字节相同。
模型接口的匿名纯客户端复现仍未成功，不把“媒体直链可下载”冒充“模型接口已打通”。

## 直接观察与目标身份

同一原始官方APK7.3.9.32、redroid Android14、ARM native bridge，临时隔离，无账户登录。
只读观察JSON解析输入及结果，保留原方法返回值；没有消费网络响应流、修改模型或绕过证书/授权检查。发现Gson经包内增强方法`Gson__fromJson$___twin___`走正常解析，包含Reader输入/返回对象路径，不能只监听String输入。
最初含目标数字ID的约70KB数据实际是分集目录，已经纠正；目录`video_list`不能当作含媒体编码的VOD模型。
打开官方离线选集面板触发`multi_video_model/default/v1`预取（仅模型）；响应结构是`data[分集数字ID].video_model`，后者为JSON字符串。以`data[7655636095014538302]`定位第81集，不拿时长接近的其他分集冒充。
目标离线模型：video_duration=86.099，video_list仅两档：
- 360p，H.264，640×360，9939601字节。
- 720p，H.264，1280×720，20347168字节。
此模型内没有1080P候选，但这不能证明所有接口/版本均不存在1080P。
官方单集播放模型另见360/480/540/720P ByteVC2；本轮观察证据支持播放与离线模型不同，不再依据加载解码库推断具体编码。

## 请求差异和匿名复现

构建和Retrofit响应URL仍为reading.snssdk.com `/novel/player/multi_video_model/default/v1`，业务字段与research.md一致；不能视为已经定位TTNet内部最终调度主机。
官方query有73932/7.3.9.32、Android14、渠道seo_laxin_pc_android，以及`cdid`、`dragon_device_type`、`host_abi`、`pv_player`等额外字段。原始值只留临时目录。
请求构建头包含Accept(JSON与protobuf)、gzip、x-reading-request、lc、sdk/passport版本及区域头。只保留名称，不保存设备、签名或不明头值到仓库。
本轮最小对照：
1. 公开源码提供的preload，以及default/player三条批量路径，项目匿名客户端均返回空正文。
2. Accept增加protobuf或去掉项目签名、加入device_level=3，仍空正文。
3. 使用实验App完整匿名query（含其自行生成的设备参数），单集目标正文，项目重新签名，reading与api5两主机仍空正文。
4. 重放构建层捕获的gzip正文/头/query（仅批量模型预取，不下载其他集），仍HTTP200、0字节。构建层头不等于TTNet最终签名头，不能称已做最终网络请求的精确重放。
所有请求探针PASS只代表执行完成，不能记为媒体成功。本轮媒体成功由另一个下载+解码探针验证。

## TTNet观察限制

APK中实际Cronet命名空间是`com.ttnet.org.chromium.net.impl`。读取到CronetUrlRequest存在`addSecurityFactor(String,String[])`等方法，只能证明发送层另有安全因子处理入口，不证明空正文的根因就是缺签名。
尝试只读观察其构造/调用遇到Frida分配页失败、后续TransportError，没有得到最终网络URL或安全因子返回值。未绕过检测或修改安全因子，停止该路径。还存在类加载时机问题，冷启动太早会无法读取二级Dex类。
剩余阻断：最终调度地址、最终公共头/签名与匿名模型请求的必要条件未定位。不能断言登录可解，也不能断言修改一个编码参数即可解。

## 对项目的最小接入方向（尚未实现）

将Android职责收窄到获取官方离线模型。适配层必须按返回Map的目标分集数字ID核对并取video_model，随后复用parseDownloadAppMedia、DownloadRequest及现有落盘/媒体校验。
本轮现有Go代码在模型已获得后即可选720P H.264并完成下载；无需在Android中下载整集或进行ByteVC2转码。
如果继续实施，Android兜底应只在当前正常兼容来源耗尽后、排队列重试/最终失败前触发，并受现有取消/重试预算和有限并发约束；本轮没有授权生产实现，没有改下载链。Android仍是模型获取依赖，稳定运行、批量排队、授权/设备生命周期、其他分集及长期稳定性未验证。

## 公开源码线索

Gread核对zhenyong97/hongguo-downloader `src/native/hongguo.js`：MODEL_BIZ_PARAM有video_platform1024；fetchPlayUrlSingle使用multi_video_model/preload/v1和mixed_video_id_map1004，并读data[vid].video_model。该仓库也处理HTTP200空正文并回退官网，不能当作批量接口可用证明。
来源：https://github.com/zhenyong97/hongguo-downloader
没有采用其硬编码设备身份或解密实现，没有新增依赖。

## 独立复核和清理

独立核验6条包含目标分集Map条目的模型消息：目标条目一致，抽取模型与原返回一致；文件SHA256与既有成品相同，FFprobe确认为H.264/AAC，完整FFmpeg音视频解码退出0、未传密钥。目录数据误判已纠正，结论限定为官方App获得模型。
停止所有本轮观察器、专用ADB5038，删除本轮容器/镜像/临时APK/工具/原始请求/模型/overlay/测试媒体副本；复查5038、5557、27043端口关闭，容器不存在。只保留既有仓库外成品和脱敏记录；并行Emby工作未触碰，无生产代码改动，无提交推送。
