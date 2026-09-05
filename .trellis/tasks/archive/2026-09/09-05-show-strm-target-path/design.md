# 详情页 STRM 真实路径设计

## Boundary

- 后端新增 `GET /media/:id/strm-target`，响应仅包含 `{ "target": string }`。
- handler 先按当前请求的媒体可见性加载媒体，再调用 service 层读取已确认媒体行的 `.strm` 文件。
- service 层复用现有 `readLocalSTRMTarget`，不重复实现解析规则。
- 前端根据当前选中版本判断 `.strm`，用独立 effect 请求并维护目标状态。

## Data Flow

1. 用户进入详情页或切换版本。
2. 前端确定当前选中 `Media`；非 `.strm` 立即清空目标。
3. STRM 版本请求 `GET /media/:id/strm-target`。
4. 后端校验可见性和 `.strm` 类型，实时读取文件，返回唯一目标字段。
5. 前端仅接受仍对应当前 media ID 的响应，并在本地路径下方展示。

## Errors and Compatibility

- 不存在、不可见、非 STRM 或无有效目标统一不返回目标；文件读取错误由现有 handler 错误约定处理。
- 前端吞掉该辅助信息请求的失败，避免阻断详情页。
- 原媒体详情与版本接口响应保持不变。

## Rollback

- 删除新路由、handler/service 方法、前端 API/effect 和展示行即可完整回滚，无数据迁移。
