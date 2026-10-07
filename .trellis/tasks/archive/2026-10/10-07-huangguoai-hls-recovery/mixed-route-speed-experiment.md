# 主入口双路与主备用各一路隔离测速（2026-10-07）

## 结论
三部作品、每种方案三轮、本轮总并发相同为2时，主+备用混用的汇总速度没有超过主入口双路。单轮有混用更快的情况，但不具备稳定优势；898混用备用1出现一次第105片读取超时。

本次支持“主备用同时用于不同分片在抽样传输上可行，但没有证明额外提速”，不支持“备用永远更慢”“备用是独立CDN”或“该策略可直接发布完整影片”。建议优先验证主入口的有界分片并发，备用作为故障兜底；本轮未实施或部署这些策略。

## 分组与验收
- 复用当前项目Detail/Resolve/ParsePlaylist及DownloadHTTPClient/DownloadRequest。每个入口重新解析对应同作品第1集，使用该入口对应Referer，禁止手工替换媒体host或媒体签名。
- 三个入口：主入口huangguoai.com；备用1为参考项目的ttvoij入口；备用2为thu入口。URL及签名始终仅内存保存。
- 样本2378全部8片，1022的1/6/11/16/21/26/31/36片，898的1/21/42/62/83/105/124/145片。898覆盖此前读取中断的第105片。
- 核对页面作品身份、标题、清单段数/总时长/媒体序列；对抽样段比较时长、IV、中断标记、有无密钥和初始化段，以及实际密钥/初始化段SHA256。非零字节范围样本拒绝；此次三路均通过抽样兼容性检查。
- KeyGeneration是解析时KEY标签的序号，不作为跨入口密钥身份凭据；本实验以抽样片实际密钥内容、IV和媒体序列核对加密条件。没有核对未抽样片，兼容性结论仅限抽样段。
- 先顺序读取主入口每个样本作为字节长度和SHA256基准（24个完整分片）。测速阶段每个完整响应都要匹配此内存基准，未输出hash或密钥。
- 主入口双路：两个worker均用主入口。混用：一个worker用主入口，另一个用指定备用。两worker各处理固定的4片分区，总并发上限2，使用相同分区规则；每轮交换奇偶分区给主备用。
- 三轮顺序分别为主/混备用1/混备用2、混备用1/混备用2/主、混备用2/主/混备用1。每组新建同种guarded client，计时包括连接及响应读取，不包括前置解析与基准/密钥读取；每片25秒、一次请求，没有重试或自动换源。
- 三方案总计216次计时分片请求，215次完整且长度及SHA256一致，1次读取超时，无内容不一致。所有记录中的两路分配数量符合8+0或4+4；完整结果及分组计数由脚本再次断言核对。

## 结果
表中速度以三轮总完整字节数除三轮总耗时计算（MiB/s），不使用单轮最好成绩。任一轮含失败的组不报完整传输速度，保留失败率与耗时。主/备标签表示解析入口，不代表已经确认物理上独立的传输线路。

|样本|主入口双路|主＋备用1|主＋备用2|
|---|---:|---:|---:|
|2378|28.65（24/24完整）|22.12（24/24完整）|15.45（24/24完整）|
|1022|15.33（24/24完整）|13.56（24/24完整）|12.03（24/24完整）|
|898|23.17（24/24完整）|1/24未完整|3.73（24/24完整）|

898混备用1的失败轮耗时25.873秒，第105片读取932957字节后达到25秒限时；该片主入口基准完整长度为2223488字节。同方案另外两轮全部完成，说明不能推断此备用地址永久不可用。

## 局限与复核
- 本工作机真实公网请求，不等于实际生产下载主机。未核验CDN节点或连接IP相同/不同，也不把签名或路径变化推断为独立带宽。
- 仅短批次、三轮重复；网络时段、连接建立和上游缓存会影响结果。主入口预读取可能预热缓存，轮换顺序无法彻底消除此偏差。速度仍有大幅波动，不承诺稳定倍数。
- 1022/898只抽样8片，2378测到全部8个传输分片。没有拼接产物、解密/合并/全轨解码或故事/口型人工验收；本次内容一致只说明被测分片传输字节一致。
- 25秒观察窗与生产任务的长超时、资源重试不同，不能将一次测速超时认作生产最终失败。
- 独立只读审查检查了总并发、固定分区、数据竞争、hash/密钥核对及隐私输出，未发现影响本次对照的阻塞问题。审查提醒兼容性限于抽样、少量轮次不代表稳定加速，以及empty/size可能未进入分类汇总。此次唯一失败category=timeout，分类总数和bad一致；报告以bad和逐条FAIL为准，失败字节不计有效速度。
- 第二次独立只读复核逐组重新计算速度及216/215/1计数，表格和结论均通过；主代理也确认报告原始数字与实验日志逐行一致。
- 测速外没有改动生产设置、队列、服务或下载代码；仅新增研究计划和本报告。没有启动服务，临时测试脚本与本地数值日志在最终复核后删除，原有其他任务和未提交修改保留。

## 原始数字（固定标签、数字、分类与布尔）

```text
RESOLVE id=2378 route=0 ok=true segments=8
RESOLVE id=2378 route=1 ok=true segments=8
RESOLVE id=2378 route=2 ok=true segments=8
COMPATIBLE id=2378 route=0 ok=true
COMPATIBLE id=2378 route=1 ok=true
COMPATIBLE id=2378 route=2 ok=true
BASELINE id=2378 segment=1 bytes=7048880 category=ok
BASELINE id=2378 segment=2 bytes=7196464 category=ok
BASELINE id=2378 segment=3 bytes=7181424 category=ok
BASELINE id=2378 segment=4 bytes=6776096 category=ok
BASELINE id=2378 segment=5 bytes=7069744 category=ok
BASELINE id=2378 segment=6 bytes=6891520 category=ok
BASELINE id=2378 segment=7 bytes=7070688 category=ok
BASELINE id=2378 segment=8 bytes=5568944 category=ok
MIXED id=2378 round=1 mixed=0 concurrency=2 bytes=54803760 valid_bytes=54803760 seconds=0.615 MiBps=85.024 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=8 backup1_count=0 backup2_count=0
MIXED id=2378 round=1 mixed=1 concurrency=2 bytes=54803760 valid_bytes=54803760 seconds=2.315 MiBps=22.575 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=4 backup2_count=0
MIXED id=2378 round=1 mixed=2 concurrency=2 bytes=54803760 valid_bytes=54803760 seconds=5.889 MiBps=8.876 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=0 backup2_count=4
MIXED id=2378 round=2 mixed=1 concurrency=2 bytes=54803760 valid_bytes=54803760 seconds=2.939 MiBps=17.782 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=4 backup2_count=0
MIXED id=2378 round=2 mixed=2 concurrency=2 bytes=54803760 valid_bytes=54803760 seconds=2.396 MiBps=21.812 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=0 backup2_count=4
MIXED id=2378 round=2 mixed=0 concurrency=2 bytes=54803760 valid_bytes=54803760 seconds=1.599 MiBps=32.686 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=8 backup1_count=0 backup2_count=0
MIXED id=2378 round=3 mixed=2 concurrency=2 bytes=54803760 valid_bytes=54803760 seconds=1.862 MiBps=28.070 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=0 backup2_count=4
MIXED id=2378 round=3 mixed=0 concurrency=2 bytes=54803760 valid_bytes=54803760 seconds=3.258 MiBps=16.040 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=8 backup1_count=0 backup2_count=0
MIXED id=2378 round=3 mixed=1 concurrency=2 bytes=54803760 valid_bytes=54803760 seconds=1.834 MiBps=28.505 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=4 backup2_count=0
RESOLVE id=1022 route=0 ok=true segments=36
RESOLVE id=1022 route=1 ok=true segments=36
RESOLVE id=1022 route=2 ok=true segments=36
COMPATIBLE id=1022 route=0 ok=true
COMPATIBLE id=1022 route=1 ok=true
COMPATIBLE id=1022 route=2 ok=true
BASELINE id=1022 segment=1 bytes=1759504 category=ok
BASELINE id=1022 segment=6 bytes=3506016 category=ok
BASELINE id=1022 segment=11 bytes=3362208 category=ok
BASELINE id=1022 segment=16 bytes=1632976 category=ok
BASELINE id=1022 segment=21 bytes=1640880 category=ok
BASELINE id=1022 segment=26 bytes=1843168 category=ok
BASELINE id=1022 segment=31 bytes=1429936 category=ok
BASELINE id=1022 segment=36 bytes=483920 category=ok
MIXED id=1022 round=1 mixed=0 concurrency=2 bytes=15658608 valid_bytes=15658608 seconds=1.319 MiBps=11.319 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=8 backup1_count=0 backup2_count=0
MIXED id=1022 round=1 mixed=1 concurrency=2 bytes=15658608 valid_bytes=15658608 seconds=0.972 MiBps=15.356 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=4 backup2_count=0
MIXED id=1022 round=1 mixed=2 concurrency=2 bytes=15658608 valid_bytes=15658608 seconds=0.639 MiBps=23.371 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=0 backup2_count=4
MIXED id=1022 round=2 mixed=1 concurrency=2 bytes=15658608 valid_bytes=15658608 seconds=1.435 MiBps=10.407 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=4 backup2_count=0
MIXED id=1022 round=2 mixed=2 concurrency=2 bytes=15658608 valid_bytes=15658608 seconds=2.229 MiBps=6.700 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=0 backup2_count=4
MIXED id=1022 round=2 mixed=0 concurrency=2 bytes=15658608 valid_bytes=15658608 seconds=0.869 MiBps=17.192 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=8 backup1_count=0 backup2_count=0
MIXED id=1022 round=3 mixed=2 concurrency=2 bytes=15658608 valid_bytes=15658608 seconds=0.857 MiBps=17.419 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=0 backup2_count=4
MIXED id=1022 round=3 mixed=0 concurrency=2 bytes=15658608 valid_bytes=15658608 seconds=0.734 MiBps=20.358 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=8 backup1_count=0 backup2_count=0
MIXED id=1022 round=3 mixed=1 concurrency=2 bytes=15658608 valid_bytes=15658608 seconds=0.898 MiBps=16.634 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=4 backup2_count=0
RESOLVE id=898 route=0 ok=true segments=145
RESOLVE id=898 route=1 ok=true segments=145
RESOLVE id=898 route=2 ok=true segments=145
COMPATIBLE id=898 route=0 ok=true
COMPATIBLE id=898 route=1 ok=true
COMPATIBLE id=898 route=2 ok=true
BASELINE id=898 segment=1 bytes=2161264 category=ok
BASELINE id=898 segment=21 bytes=2097712 category=ok
BASELINE id=898 segment=42 bytes=2004464 category=ok
BASELINE id=898 segment=62 bytes=2053152 category=ok
BASELINE id=898 segment=83 bytes=2226304 category=ok
BASELINE id=898 segment=105 bytes=2223488 category=ok
BASELINE id=898 segment=124 bytes=2051472 category=ok
BASELINE id=898 segment=145 bytes=1987360 category=ok
MIXED id=898 round=1 mixed=0 concurrency=2 bytes=16805216 valid_bytes=16805216 seconds=0.636 MiBps=25.217 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=8 backup1_count=0 backup2_count=0
FAIL id=898 round=1 mixed=1 route=1 segment=105 bytes=932957 category=timeout
MIXED id=898 round=1 mixed=1 concurrency=2 bytes=15514685 valid_bytes=14581728 seconds=25.873 MiBps=0.000 good=7 bad=1 mismatch=0 timeout=1 read=0 request=0 main_count=4 backup1_count=4 backup2_count=0
MIXED id=898 round=1 mixed=2 concurrency=2 bytes=16805216 valid_bytes=16805216 seconds=7.140 MiBps=2.245 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=0 backup2_count=4
MIXED id=898 round=2 mixed=1 concurrency=2 bytes=16805216 valid_bytes=16805216 seconds=2.848 MiBps=5.627 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=4 backup2_count=0
MIXED id=898 round=2 mixed=2 concurrency=2 bytes=16805216 valid_bytes=16805216 seconds=5.019 MiBps=3.193 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=0 backup2_count=4
MIXED id=898 round=2 mixed=0 concurrency=2 bytes=16805216 valid_bytes=16805216 seconds=0.771 MiBps=20.775 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=8 backup1_count=0 backup2_count=0
MIXED id=898 round=3 mixed=2 concurrency=2 bytes=16805216 valid_bytes=16805216 seconds=0.746 MiBps=21.481 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=0 backup2_count=4
MIXED id=898 round=3 mixed=0 concurrency=2 bytes=16805216 valid_bytes=16805216 seconds=0.668 MiBps=23.986 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=8 backup1_count=0 backup2_count=0
MIXED id=898 round=3 mixed=1 concurrency=2 bytes=16805216 valid_bytes=16805216 seconds=0.678 MiBps=23.644 good=8 bad=0 mismatch=0 timeout=0 read=0 request=0 main_count=4 backup1_count=4 backup2_count=0
```
