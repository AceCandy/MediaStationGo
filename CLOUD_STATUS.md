# MediaStationGo 云盘功能状态报告

## 已实现的功能

### 1. 云盘存储支持
- ✅ 支持的云盘类型：
  - Quark (夸克网盘)
  - 115 (115网盘)
  - CloudDrive2 (桥接网盘)
  - OpenList/AList (兼容网盘)

### 2. 播放机制
- ✅ HTTP 302 重定向播放（直连云盘CDN）
- ✅ STRM 文件支持
- ✅ Emby 协议兼容（标记 IsRemote=true）
- ✅ Token 认证传递

### 3. CORS 支持
- ✅ 支持的 Header：
  - Authorization
  - Content-Type
  - X-Requested-With
  - X-Emby-Token
  - X-MediaBrowser-Token
  - X-Emby-Authorization

### 4. 图片代理
- ✅ 云盘图片缓存
- ✅ Emby API 图片端点：/Items/{id}/Images/Primary
- ✅ 直接图片输出（避免 token 401）

### 5. 启动健康检查
- ✅ 云盘配置验证
- ✅ Ping 测试连通性
- ✅ 自动扫描云盘媒体库

### 6. 存储管理
- ✅ 删除云盘时清理媒体库
- ✅ 停止相关扫描任务

## 可能的问题原因

### 第三方播放器无法播放

**可能原因：**
1. **云盘未配置** - 检查是否添加了云盘存储配置
2. **Cookie 过期** - 云盘 Cookie 需要定期更新
3. **CORS 配置** - 生产环境需要配置 cors_origins
4. **网络问题** - 云盘 CDN 网络不可达

**诊断步骤：**
\\\ash
# 1. 运行诊断脚本
./diagnose-cloud.ps1

# 2. 检查日志中的错误
docker logs mediastation-go 2>&1 | grep -i "cloud storage"

# 3. 检查 CORS 配置
# 编辑 config.toml，添加：
# cors_origins = ["*"]  # 或具体的播放器域名
\\\

### 云盘海报不显示

**可能原因：**
1. **图片路径格式错误** - 应该是 /api/cloud/play/{type}?ref={path}
2. **图片未缓存** - 首次加载可能较慢
3. **云盘权限问题** - Cookie 权限不足

**检查方法：**
\\\ash
# 查看图片代理日志
docker logs mediastation-go 2>&1 | grep -i "image"

# 测试图片 URL（需要替换 token）
curl -H "X-Emby-Token: YOUR_TOKEN" http://localhost:8080/Items/MEDIA_ID/Images/Primary
\\\

### 媒体库重复扫描

**说明：**
- 每个用户首次访问会触发扫描
- 云盘媒体库在启动时自动扫描
- 扫描结果对所有用户共享

**优化建议：**
- 使用调度任务定期扫描
- 避免手动触发多次扫描

## 配置示例

### 1. 添加云盘存储

通过 Web UI 的"外部存储"页面添加：
- 选择云盘类型（Quark/115/CloudDrive2）
- 输入 Cookie（从浏览器开发者工具获取）
- 测试连接
- 启用存储

### 2. 创建云盘媒体库

媒体库路径格式：
\\\
cloud://{provider}:{scan_dir}:{display_dir}
\\\

示例：
\\\
cloud://quark:/电影:/Movies
cloud://cloud115:/剧集:/TV Shows
\\\

### 3. CORS 配置（针对第三方播放器）

编辑 config.toml：
\\\	oml
[app]
cors_origins = [
  "*",  # 允许所有来源（开发环境）
  # 或指定具体域名（生产环境）:
  # "https://app.emby.media",
  # "app://infuse"
]
\\\

## 最新改进（本次提交）

1. **存储删除功能** - 删除云盘时自动清理关联的媒体库和媒体项
2. **健康检查** - 启动时验证所有云盘配置，及早发现问题
3. **扫描验证** - 扫描前检查存储配置是否存在，避免无效扫描

## 下一步建议

### 对于用户：
1. 运行诊断脚本：./diagnose-cloud.ps1
2. 检查是否配置了云盘存储
3. 验证 Cookie 是否有效
4. 配置 CORS（如果使用第三方播放器）

### 对于开发：
1. ✅ 添加更详细的错误日志
2. ✅ 启动时健康检查
3. 🔄 优化扫描缓存机制
4. 🔄 添加 Cookie 自动刷新
5. 🔄 改进错误提示

## 参考项目对比

### MoviePilot 云盘方案
- 使用 CloudDrive/Alist 作为统一桥接层
- STRM 文件 + 302 重定向
- 支持本地缓存

### Nowen-Video 云盘方案
- 直接集成各云盘 API
- WebDAV 挂载
- 支持多种播放模式

### MediaStationGo 优势
- 原生多云盘支持（无需额外桥接工具）
- Emby API 完全兼容
- 轻量级架构
- 自动健康检查

---

**更新时间：** 2026-06-11 12:55:56
**版本：** main@7ef4af6
