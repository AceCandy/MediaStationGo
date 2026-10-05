# 设计

在 Emby.Items 公共出口对 IsFavorite 最终页批量补充 LibraryIds，普通/红果使用已维护归属，NULL 历史记录回查页内绑定，NFO 使用所属库。按现有权限过滤且排序去重；不改变条目/排序/计数/分页。Views 的 LibraryType 是独立可选扩展。

LinPlayer Go rawItem/Item 原样传输 library_ids/library_type，可选字段省略保持旧差分语料。Android Item/View 解析；收藏在当前会话缓存红果库 ID，出现带归属收藏时按需读取 Views，失败降级原分组且不阻塞收藏。分类页可返回与本地排序复用既有组件。

固定标题栏排序入口使用对称 padding；收藏项 animateItem 使用系统动画刻度；已有 NetImage 淡入保留。两仓修改可分别回滚，新 App + 旧服务端、旧 App + 新服务端均兼容。
