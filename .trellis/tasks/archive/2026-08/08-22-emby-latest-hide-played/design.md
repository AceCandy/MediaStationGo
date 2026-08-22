# 技术设计

## 边界

改动限定在 Emby `Latest` 请求解析、作品查询与管理端接口目录，不引入用户偏好模型或数据库迁移。

## 数据流

```text
HTTP IsPlayed（缺省=false）
  → handler 解析为 bool
  → EmbyService.LatestItems
  → media 查询按 user_id + metadata_id 关联 playback_histories.completed
  → 先过滤，再按 DateCreated 排序和 Limit 分页
  → 生成现有 Emby 媒体项数组
```

`IsPlayed=true` 使用已完成历史存在条件；`false` 使用已完成历史不存在条件，因此没有历史和未完成历史都属于未播放完成。剧集查询在 Series 归组前应用同一条件。

## 缓存

播放状态是频繁变化的用户数据。为避免在进度上报路径增加全前缀缓存删除，也避免短 TTL 内返回已看作品，带播放过滤的 `Latest` 不读取或写入现有结果缓存。该端点默认始终带过滤，因此当前 `Latest` 缓存路径不再参与这些请求。

## 兼容性

- 保留现有路径、响应数组形状、ParentId、Limit、用户可见性和排序行为。
- `IsPlayed` 接受现有 `firstQueryValue` 支持的参数名大小写变体。
- 不识别的布尔值按缺省 `false` 处理，保持现有兼容层对无效可选参数的宽松风格。

## 回滚

代码和目录说明可直接回滚，无数据库变化。
