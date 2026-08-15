# 媒体详情展示型下拉框实施计划

## Success Criteria

详情页基础信息不被探测阻塞，收藏按钮位于标题旁，版本与三类轨道以四个原生下拉框紧凑展示；所有选择只影响展示，不改变任何播放目标。

## Ordered Checklist

### 1. 增加可见版本查询

- [x] 在媒体服务中增加按当前媒体 ID 和 `MediaVisibility` 查询版本的方法，复用 `FindByMetadataID`。
- [x] 当前 `MediaView` 不存在或不可见时不返回任何版本，不扩大现有详情读模型范围。
- [x] 注册 `GET /api/media/:id/versions`，复用现有请求可见性规则并保持不可见资源返回 `404`。
- [x] 在前端 `mediaAPI` 增加对应的只读调用。

Verify：定向测试覆盖同一 `MetadataID` 的多个版本，以及隐藏媒体库/NSFW 版本不泄露；路由注册测试通过。

Rollback point：移除新增服务方法、处理器、路由和前端 API 方法，不影响现有详情接口。

### 2. 分离基础媒体与展示版本状态

- [x] 在 `useMediaDetailPageState` 中加载版本列表，失败时退化为当前媒体。
- [x] 增加当前展示版本状态；选择非 URL 版本时读取详情并按需调用现有 `ensureProbe`。
- [x] 使用 effect 取消标记防止快速切换时旧请求覆盖新选择。
- [x] 保持 `media` 和现有操作回调绑定 URL 媒体；不得把展示版本传给播放、投屏、收藏或管理操作。
- [x] 版本加载和探测状态只暴露给媒体选择区域，不重新触发全页 loading。

Verify：人工检查快速切换版本、切回当前版本、探测成功/失败和版本接口失败；播放链接始终保持 `/play/<URL媒体ID>`。

### 3. 替换详情页展示

- [x] 将收藏按钮从播放操作区移到 `MediaDetailMetadata` 标题旁，复用现有样式和回调。
- [x] 将 `MediaDetailTracks` 的纵向列表替换为版本、视频、音频、字幕四个原生选择器。
- [x] 为无轨道、加载中、探测中和探测失败提供局部且可访问的状态。
- [x] 通过现有 `MediaDetailPageSections` 和 `MediaDetailPage` 透传最少必要 props。
- [x] 保持播放、外部播放器、投屏、管理员操作和对话框继续接收基础 `media`。

Verify：390px、768px 与桌面宽度无水平溢出；Tab 键可聚焦四个选择器和收藏按钮；纵向轨道列表不再出现。

### 4. 定向验证与独立复核

- [x] 运行新增或受影响的 Go handler/service 定向测试，不执行全量 Java 编译或无关测试。
- [x] 在 `web/` 运行 `npm run lint` 和 `npm run build`。
- [x] 运行 `git diff --check`。
- [x] 独立复核需求映射、可见性过滤、异步竞态、播放目标不变和现有未提交探测改动未被覆盖。
- [x] 若启动本地服务或浏览器调试，结束前关闭服务，并删除含真实媒体信息的截图或临时产物。

Suggested commands：

```bash
go test ./internal/handler ./internal/service
cd web && npm run lint
cd web && npm run build
git diff --check
```

## Review Gates

- 不得让任一下拉选择改变播放、外部播放器、投屏或管理操作目标。
- 不得通过全媒体库列表或标题启发式规则实现详情版本查询。
- 不得让版本列表或探测请求重新阻塞详情首屏。
- 不得新增下拉组件、状态管理或播放器依赖。
- 不得覆盖本工作树中尚未提交的详情探测和媒体库分页改动。
