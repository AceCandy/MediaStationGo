# 技术设计

## 边界与不变量

- `Media` 继续表示一个可独立播放的物理文件，现有路径唯一性、探测、删除和进度链路不变。
- multipart 表示同一播放版本内的有序文件；多版本继续表示同一作品的不同来源/画质。
- Part 关系只在至少两个合法候选存在时生效，避免单文件标题误判。

## 数据模型

在 `model.Media` 增加：

- `PartGroupKey string`：库内稳定组键，由目录、去掉 Part 后缀的基础名和 Part 类型规范化得到。
- `PartIndex int`：数字直接使用，`a-d` 映射为 `1-4`；非 Part 为 `0`。

不保存 `PartCount`、父媒体 ID或额外关系表。组内首个序号是主 Part，数量和主项通过查询推导，避免删除或重扫造成冗余字段失真。

## 扫描数据流

1. 新的纯函数解析文件名尾部，返回基础名、Part 类型和顺序。
2. 扫描仍先按物理路径写入媒体。
3. 批次写入和缺失文件清理完成后，对扫描范围内媒体重新解析并按目录、基础名、类型分组。
4. 仅合法且成员不少于两个的组写入 `PartGroupKey/PartIndex`；其余行清空这两个字段。
5. 生效组的扫描标题使用去掉 Part 后缀的基础名清洗；解除组时恢复按原文件名计算的扫描标题。
6. Part 调和在自动刮削启动前完成，使新文件使用正确的作品/单集查询身份。

全量扫描和根扫描复用同一调和函数；不在逐文件 `ingestFile` 中建立最终关系。

## 多版本与播放查询

- `mediaVersionSiblings` 与列表版本折叠只把普通媒体和 Part 组主项作为版本候选。
- 请求主 Part/逻辑条目时，PlaybackInfo 返回所有版本的主 Part 作为 `MediaSources`。
- 请求后续 Part 的具体媒体 ID 时，只返回该物理 Part，不重新扩展为所有版本。
- 按 `PartGroupKey` 查询时继续应用现有用户可见性过滤并按 `PartIndex` 排序。

## 兼容 API

- Item DTO：当前/首选版本存在有效 Part 组时增加 `PartCount`。
- 新增 `GET /Videos/:id/AdditionalParts` 以及项目已有 `/emby`、无前缀和小写兼容入口。
- 响应保持 Jellyfin 风格 `{Items, TotalRecordCount}`，只返回主 Part 之后的成员。
- Additional Part DTO 覆盖为具体媒体 ID，并只携带自己的 `MediaSources`；现有 token 附加逻辑复用到响应 `Items`。
- 具体 Part 继续使用已有 `/Videos/:id/stream`、PlaybackInfo 和进度接口。

## 兼容、迁移与回滚

- 数据库通过现有 AutoMigrate 添加可空/零值字段；旧行默认保持现状。
- 用户重新扫描媒体库后建立 Part 关系，不做启动期全库数据改写。
- 回滚代码后新增列可保留且不影响旧版本；无需破坏性数据库回滚。

## 风险控制

- 误识别：要求尾部边界、支持列表内 token、同目录至少两个成员、序号唯一。
- 多版本混淆：在版本查询入口统一排除非主 Part，并添加“多版本 × 多 Part”回归测试。
- 删除残留：扫描调和对范围内所有候选同时写入或清空关系，不依赖父 ID。
- 权限泄漏：AdditionalParts 查询复用 `mediaQueryFilter`，不直接裸查并返回媒体。
