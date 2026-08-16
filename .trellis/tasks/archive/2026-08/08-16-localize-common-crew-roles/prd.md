# 本地化常见影视职务

## Goal

在 Emby 人物列表输出时本地化常见导演和编剧职务，同时保持 Type 协议枚举不变。

## Requirements

- Emby 人物条目的 `Role` 对常见影视职务使用中文展示。
- 映射范围为 `Director`、`Writer`、`Screenplay`、`Story`、`Teleplay`。
- `Type` 必须继续返回 Emby 兼容的英文枚举。
- 历史人物关系无需重新刮削即可生效。
- 未知或已经本地化的职务保持原值。

## Acceptance Criteria

- [x] `Director` 显示为“导演”。
- [x] `Writer` 和 `Screenplay` 显示为“编剧”。
- [x] `Story` 显示为“故事创作”。
- [x] `Teleplay` 显示为“电视剧编剧”。
- [x] `Actor`、`GuestStar` 的现有角色译文及未知职务不变。
- [x] `Type` 仍为 `Actor`、`GuestStar`、`Director` 或 `Writer`。
- [x] 定向 Go 单元测试通过。

## Notes

- 本任务只改变播放器可见的角色展示文本，不改变路由、字段结构或数据库内容。
