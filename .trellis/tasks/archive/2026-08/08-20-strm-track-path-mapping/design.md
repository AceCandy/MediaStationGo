# STRM 远程轨道路径映射设计

## Boundary

新增设置 `ffprobe.path_mappings`，前端标签为“提取轨道路径映射”。每行格式：

```text
远程 HTTP(S) URL 前缀 => 本地路径前缀
```

它只影响 `MediaProbeService` 选择 FFprobe 输入源，不改变播放、302、STRM 内容、媒体记录或外挂字幕。

## Data Flow

```text
STRM 远程 URL
  → 读取 ffprobe.path_mappings
  → 选择最长的有效 URL 前缀
  → 将 URL 剩余路径拼到本地路径前缀
  → 本地文件可访问：Probe(path)
  → 本地文件不可访问/未命中：ProbeHTTP(url)
  → 现有 persist 一致性校验与写入
```

## Mapping Contract

- 空行、`#` 注释和格式非法的行与现有播放路径映射一样被忽略。
- 远程前缀必须是带 host 的 HTTP(S) URL，本地前缀必须是绝对路径。
- 匹配 scheme、host 和 URL path；查询参数与 fragment 不参与本地路径构造，避免签名参数进入文件名。
- URL path 按解码后的路径拼接，并验证结果仍位于配置的本地前缀下，阻止 `..` 或编码路径逃逸。
- 多条规则匹配时使用最长远程 URL path 前缀；配置顺序不影响更具体的规则。

## Service Integration

- 在 `MediaProbeService.resolveSource` 处仅对容器标记为 STRM 或路径扩展名为 `.strm` 的 HTTP(S) URL 尝试映射，命中且本地文件可用时返回现有 `localMediaProbeSource`。
- 复用同一映射方法计算 `currentSourceIdentity`；如果映射配置或本地文件在探测期间变化，现有 `ErrMediaProbeSourceChanged` 会拒绝过期结果。
- 初始解析时映射文件不可用则保留远程源，继续现有 2–5 秒节流和 `ProbeHTTP`。

## Compatibility and Rollback

- 新设置默认为空，无数据迁移；未配置时行为与当前完全一致。
- 回滚代码即恢复旧行为；数据库中留存的未识别 setting 不会被消费，也不影响其他功能。

## Risks

- 配置映射到错误但存在的本地文件时，FFprobe 会提取该文件。通过最长前缀、路径边界校验和源身份校验降低风险；配置正确性仍由管理员负责。
