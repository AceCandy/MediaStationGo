# 备用入口与分片并发隔离测速（2026-10-07）

## 结论
备用入口可以重新解析并下载本次抽样的同内容分片，但没有证明稳定提速：前两个样本第二备用入口相对主入口分别约快2%、3%，第一备用入口略慢；第三样本备用入口出现未完整读取。不能作为独立CDN加速线路的证据。

主入口分片并发存在提升空间，但并发2也遇到一次不完整读取，且预试验并发3曾耗时14.580秒、同组串行另一次仅0.385秒。不能承诺普遍提速或固定倍数。没有修改生产设置或实现并发下载。

## 方法与边界
- 本工作机真实公网HTTP请求，使用项目DownloadHTTPClient公网地址/重定向守卫及DownloadRequest；未操作生产队列、服务或配置。
- 主入口、参考项目的ttvoij入口（备用1）、thu入口（备用2）分别调用当前Detail/Resolve解析同作品第1集。页面作品身份/集号有效、标题一致、清单段数/总时长/抽样段时长一致。
- 三路媒体host/path相同，抽样分片host/path组合存在差异。此差异不等于独立CDN；未验证边缘节点或连接IP是否不同。
- 样本2378（8片）、1022（36片）、898（145片）各取4片：第1片、零基索引floor(N/3)、floor(2N/3)、末片；898以零基104即第105片替代末片，覆盖曾读取中断位置。
- 每组两轮，顺序第一轮主串行/备用1串行/备用2串行/主并发2/主并发3，第二轮逆序。每组新客户端，计时含连接与完整响应读取，不含前置页面/清单解析。
- 每分片限时25秒，每请求一次，无重试。仅测速分片传输，不拉密钥、不解密、不合并、不做整片完整性或播放验收。短观察窗失败不等于生产长超时与重试最终失败。
- 成功分片逐个按长度及SHA256与本次首个成功响应在内存比对，未输出hash、URL、签名、密钥或标题。120次请求116次完整、4次未完整，完整响应未发现字节不一致。
- 本表对两轮总字节/总耗时汇总；失败组不报成功吞吐。仅两个短批次，缓存、连接、网络瞬时波动均可能影响结果，不能外推生产工作机、长时间速度或整片下载耗时。

## 数值（MiB/s；未完整请求数/8）

|样本|主入口串行|备用1串行|备用2串行|主入口并发2|主入口并发3|
|---|---:|---:|---:|---:|---:|
|2378|40.02（0/8）|34.56（0/8）|40.82（0/8）|68.43（0/8）|54.56（0/8）|
|1022|16.45（0/8）|16.27（0/8）|16.97（0/8）|19.97（0/8）|20.47（0/8）|
|898|11.63（0/8）|未完整 1/8|未完整 2/8|未完整 1/8|16.67（0/8）|

## 后续建议
备用入口适合在页面不可用时兜底，暂不把它作为稳定加速方案。若实施速度优化，先做有界分片并发，并保留逐资源重试、单资源大小限制、任务取消、失败清理及最终严格校验；需要长批次、多时段和实际下载主机验证后再选默认并发。

## 复核与清理
方法已由独立只读审查核验；审查指出失败字节不可计为有效吞吐、短批次不能证明稳定加速，报告已明确排除失败组的速度结论。早期预试验因过严的分片路径相同门槛跳过备用入口，未用于备用测速比较；正式实验放行身份/时长一致线路，再用实际字节哈希核验。

临时探测测试及本地数值日志在记录报告后删除；未启动服务、未保存媒体文件。原有未提交实现及其他任务保持原状。本报告不代表代码提交、部署或功能回归完成。

## 原始数字（仅固定标签、数字、布尔）

```text
RESOLVE id=2378 route=0 ok=true segments=8
RESOLVE id=2378 route=1 ok=true segments=8
RESOLVE id=2378 route=2 ok=true segments=8
COMPARE id=2378 route=1 identity_timing_same=true segment_host_paths_same=false media_host_path_same=true
COMPARE id=2378 route=2 identity_timing_same=true segment_host_paths_same=false media_host_path_same=true
SPEED id=2378 round=1 route=0 concurrency=1 bytes=26690768 seconds=0.476 MiBps=53.459 good=4 bad=0 verified=0 mismatch=0
SPEED id=2378 round=1 route=1 concurrency=1 bytes=26690768 seconds=0.792 MiBps=32.140 good=4 bad=0 verified=4 mismatch=0
SPEED id=2378 round=1 route=2 concurrency=1 bytes=26690768 seconds=0.418 MiBps=60.962 good=4 bad=0 verified=4 mismatch=0
SPEED id=2378 round=1 route=0 concurrency=2 bytes=26690768 seconds=0.404 MiBps=63.001 good=4 bad=0 verified=4 mismatch=0
SPEED id=2378 round=1 route=0 concurrency=3 bytes=26690768 seconds=0.331 MiBps=76.837 good=4 bad=0 verified=4 mismatch=0
SPEED id=2378 round=2 route=0 concurrency=3 bytes=26690768 seconds=0.602 MiBps=42.248 good=4 bad=0 verified=4 mismatch=0
SPEED id=2378 round=2 route=0 concurrency=2 bytes=26690768 seconds=0.340 MiBps=74.786 good=4 bad=0 verified=4 mismatch=0
SPEED id=2378 round=2 route=2 concurrency=1 bytes=26690768 seconds=0.829 MiBps=30.701 good=4 bad=0 verified=4 mismatch=0
SPEED id=2378 round=2 route=1 concurrency=1 bytes=26690768 seconds=0.681 MiBps=37.370 good=4 bad=0 verified=4 mismatch=0
SPEED id=2378 round=2 route=0 concurrency=1 bytes=26690768 seconds=0.796 MiBps=31.960 good=4 bad=0 verified=4 mismatch=0
RESOLVE id=1022 route=0 ok=true segments=36
RESOLVE id=1022 route=1 ok=true segments=36
RESOLVE id=1022 route=2 ok=true segments=36
COMPARE id=1022 route=1 identity_timing_same=true segment_host_paths_same=false media_host_path_same=true
COMPARE id=1022 route=2 identity_timing_same=true segment_host_paths_same=false media_host_path_same=true
SPEED id=1022 round=1 route=0 concurrency=1 bytes=5579888 seconds=0.350 MiBps=15.186 good=4 bad=0 verified=0 mismatch=0
SPEED id=1022 round=1 route=1 concurrency=1 bytes=5579888 seconds=0.285 MiBps=18.699 good=4 bad=0 verified=4 mismatch=0
SPEED id=1022 round=1 route=2 concurrency=1 bytes=5579888 seconds=0.354 MiBps=15.041 good=4 bad=0 verified=4 mismatch=0
SPEED id=1022 round=1 route=0 concurrency=2 bytes=5579888 seconds=0.240 MiBps=22.137 good=4 bad=0 verified=4 mismatch=0
SPEED id=1022 round=1 route=0 concurrency=3 bytes=5579888 seconds=0.248 MiBps=21.462 good=4 bad=0 verified=4 mismatch=0
SPEED id=1022 round=2 route=0 concurrency=3 bytes=5579888 seconds=0.272 MiBps=19.596 good=4 bad=0 verified=4 mismatch=0
SPEED id=1022 round=2 route=0 concurrency=2 bytes=5579888 seconds=0.293 MiBps=18.181 good=4 bad=0 verified=4 mismatch=0
SPEED id=1022 round=2 route=2 concurrency=1 bytes=5579888 seconds=0.273 MiBps=19.518 good=4 bad=0 verified=4 mismatch=0
SPEED id=1022 round=2 route=1 concurrency=1 bytes=5579888 seconds=0.369 MiBps=14.427 good=4 bad=0 verified=4 mismatch=0
SPEED id=1022 round=2 route=0 concurrency=1 bytes=5579888 seconds=0.297 MiBps=17.914 good=4 bad=0 verified=4 mismatch=0
RESOLVE id=898 route=0 ok=true segments=145
RESOLVE id=898 route=1 ok=true segments=145
RESOLVE id=898 route=2 ok=true segments=145
COMPARE id=898 route=1 identity_timing_same=true segment_host_paths_same=false media_host_path_same=true
COMPARE id=898 route=2 identity_timing_same=true segment_host_paths_same=false media_host_path_same=true
SPEED id=898 round=1 route=0 concurrency=1 bytes=8802208 seconds=0.680 MiBps=12.336 good=4 bad=0 verified=0 mismatch=0
SPEED id=898 round=1 route=1 concurrency=1 bytes=8802208 seconds=0.723 MiBps=11.608 good=4 bad=0 verified=4 mismatch=0
SPEED id=898 round=1 route=2 concurrency=1 bytes=7249532 seconds=25.373 MiBps=0.272 good=3 bad=1 verified=3 mismatch=0
SPEED id=898 round=1 route=0 concurrency=2 bytes=7249532 seconds=25.241 MiBps=0.274 good=3 bad=1 verified=3 mismatch=0
SPEED id=898 round=1 route=0 concurrency=3 bytes=8802208 seconds=0.499 MiBps=16.818 good=4 bad=0 verified=4 mismatch=0
SPEED id=898 round=2 route=0 concurrency=3 bytes=8802208 seconds=0.508 MiBps=16.517 good=4 bad=0 verified=4 mismatch=0
SPEED id=898 round=2 route=0 concurrency=2 bytes=8802208 seconds=0.573 MiBps=14.642 good=4 bad=0 verified=4 mismatch=0
SPEED id=898 round=2 route=2 concurrency=1 bytes=7285867 seconds=0.609 MiBps=11.408 good=3 bad=1 verified=3 mismatch=0
SPEED id=898 round=2 route=1 concurrency=1 bytes=7183996 seconds=25.295 MiBps=0.271 good=3 bad=1 verified=3 mismatch=0
SPEED id=898 round=2 route=0 concurrency=1 bytes=8802208 seconds=0.764 MiBps=10.985 good=4 bad=0 verified=4 mismatch=0
```
