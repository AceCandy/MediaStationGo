<h1 align="center">🎬 MediaStationGo</h1>
<p align="center">
  <a href="https://github.com/ShukeBta/MediaStation">MediaStation</a> 的 Go 语言重写版 —— 您的私有家庭媒体中心。
</p>
<p align="center">
  <a href="README_EN.md">English</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/React-18-61DAFB?style=flat-square&logo=react" alt="React">
  <img src="https://img.shields.io/badge/TypeScript-5-3178C6?style=flat-square&logo=typescript" alt="TypeScript">
  <img src="https://img.shields.io/badge/SQLite-WAL-003B57?style=flat-square&logo=sqlite" alt="SQLite">
  <img src="https://img.shields.io/badge/Docker-Alpine_3.19-2496ED?style=flat-square&logo=docker" alt="Docker">
  <img src="https://img.shields.io/badge/License-GPL--3.0-blue?style=flat-square" alt="License">
</p>

---

## 为什么要重写？

原版 MediaStation 是一个 Python/FastAPI + Vue 项目。**MediaStationGo** 是从零开始的全新重构，采用更轻量的单文件部署模式：

- **后端**：Go 1.25 + Gin + GORM + SQLite（WAL 模式）
- **前端**：React 18 + Vite + Tailwind CSS + Zustand
- **分发**：约 30 MB 纯静态二进制（CGO 禁用），或跨架构 Alpine Docker 镜像

目标是在保持用户功能和界面不变的前提下，大幅降低 NAS 设备上的部署复杂度。

---

## 功能特性

### 认证与用户
- ✅ JWT 认证（admin / user 双角色）
- ✅ 首次运行自动创建管理员（`admin / admin123`，可通过 `ADMIN_INITIAL_PASSWORD` 自定义）
- ✅ 个人信息页（邮箱 / 头像 / 修改密码）
- ✅ 管理员用户表（角色提升 / 降级）
- ✅ 敏感操作审计日志（登录、媒体库操作、下载等）

### 媒体库管理
- ✅ 媒体库增删改查 + 递归文件系统扫描
- ✅ ffprobe 元数据提取（时长 / 分辨率 / 编码格式 / 容器）
- ✅ 智能文件名清洗（年份 + 季/集号识别）
- ✅ 多数据源链式刮削（按媒体库类型）：
  - 电影 → TMDb（可选 Fanart.tv 高清海报升级）
  - 电视剧 → TheTVDB（TMDb 回退）
  - 动漫 → Bangumi（TMDb 回退）
- ✅ 图片代理 + 磁盘缓存（TMDb / Bangumi / 豆瓣 / Fanart / TheTVDB）
- ✅ 电视剧 / 动漫按季分组 + 剧集列表
- ✅ fsnotify 文件系统监听（5 秒防抖合并）

### 播放
- ✅ 直链播放（支持 HTTP Range）
- ✅ HLS 按需转码（每个文件独立 ffmpeg 作业）
- ✅ 外挂字幕识别（.srt / .vtt / .ass / .ssa）+ 实时 WebVTT 转换
- ✅ 播放位置续播（每 10 秒写入）+ 首页「继续观看」
- ✅ 收藏（切换）+ 播放列表（增删改查）

### PT 站点管理
- ✅ 站点配置增删改查
- ✅ 支持 6 种 PT 站点类型：nexusphp / gazelle / unit3d / mteam / discuz / custom_rss
- ✅ 3 种认证方式：Cookie / API Key / Auth Header
- ✅ 站点连接测试
- ✅ 跨站种子搜索
- ✅ 站点扩展配置（Extra JSON：User-Agent / RSS URL / 超时 / 优先级 / 代理 / 下载器）

### 自动化
- ✅ qBittorrent 下载集成（添加 / 列表 / 删除）
- ✅ RSS 订阅 + 正则过滤 + GUID 去重 + 10 分钟轮询
- ✅ 下载文件自动分类整理

### 运维监控
- ✅ 实时事件推送（扫描 / 刮削 / 转码 / 下载 / 订阅）通过 WebSocket
- ✅ 仪表盘 `/stats`（CPU / 内存 / 磁盘 / 媒体库数量 / Goroutines）
- ✅ 实时任务面板 `/tasks`（当前 ffmpeg 作业 + qBittorrent 种子）
- ✅ NFO 导出（Kodi / Jellyfin 兼容）—— 单文件或整库
- ✅ 硬件加速编码配置：Software / NVENC / Intel QSV / VAAPI
- ✅ 单文件部署、多架构 Docker 镜像、GitHub Actions CI + GHCR

### 发现与 AI
- ✅ TMDb 发现 —— 首页热门推荐
- ✅ AI 智能搜索（OpenAI 兼容接口）—— 自然语言 → 结构化查询
- ✅ AI 推荐（基于观看历史）`GET /api/ai/recommend`

### 前端
- ✅ React SPA 代码分割：登录 / 首页 / 媒体库 / 搜索 / 收藏 / 播放列表 / 详情 / 播放器（HLS + 直链 + 字幕） / 个人信息 / 下载 / 订阅 / 统计 / 管理后台 / 站点管理 / API 配置
- ✅ WebSocket 全局通知
- ✅ 初始包体积约 250 KB / Gzip 后 83 KB（hls.js 仅在首次 HLS 播放时按需加载）

### 路线图

| 功能 | 状态 |
|------|------|
| Jellyfin / Emby 双向兼容层 | ⏳ |
| DLNA / Chromecast 投屏 | ⏳ |
| 在线字幕搜索 | ⏳ |
| 多码率 ABR 转码 | ⏳ |

---

## 快速开始

### Docker 部署

```bash
git clone https://github.com/ShukeBta/MediaStationGo.git
cd MediaStationGo

# （可选）编辑 docker-compose.yml，将您的媒体目录挂载到 /media
docker compose up -d
```

打开 <http://localhost:8080>，使用 `admin / admin123` 登录。

### 裸机部署

```bash
# 前置要求：Go 1.25+、Node 20+、ffmpeg
make build       # 生成 bin/mediastation-go 和 web/dist
./bin/mediastation-go
```

### 本地开发

```bash
make dev         # 后端启动在 :8080，MEDIASTATION_APP_DEBUG=true
make dev-web     # Vite 开发服务器启动在 :3000，代理 /api → :8080
```

---

## 配置说明

配置层级：默认值 < `config.yaml` < `config/*.yaml` < 环境变量（前缀 `MEDIASTATION_`）。

### 常用环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `MEDIASTATION_APP_PORT` | `8080` | HTTP 监听端口 |
| `MEDIASTATION_APP_DATA_DIR` | `./data` | 数据目录（数据库 / 缓存 / JWT 密钥） |
| `MEDIASTATION_APP_WEB_DIR` | `./web/dist` | 前端 SPA 静态文件目录 |
| `MEDIASTATION_DATABASE_DB_PATH` | `./data/mediastation.db` | SQLite 数据库文件路径 |
| `MEDIASTATION_SECRETS_JWT_SECRET` | *(自动生成)* | JWT 签名密钥 |
| `MEDIASTATION_SECRETS_TMDB_API_KEY` | *(空)* | 启用 TMDb 电影刮削 |
| `MEDIASTATION_SECRETS_BANGUMI_ACCESS_TOKEN` | *(空)* | 可选，提升 Bangumi 速率限制 |
| `MEDIASTATION_APP_CORS_ORIGINS` | *(空)* | 跨域白名单（JSON 数组） |
| `ADMIN_INITIAL_PASSWORD` | `admin123` | 初始管理员密码 |

### 运行时设置（管理后台 → 设置）

这些配置存储在 `settings` 表中，可通过管理 UI 编辑：

| 键 | 说明 |
|----|------|
| `qbittorrent.url` | qBittorrent Web UI 地址 |
| `qbittorrent.username` | qBittorrent 用户名 |
| `qbittorrent.password` | qBittorrent 密码 |
| `qbittorrent.savepath` | 可选，新种子默认保存路径 |

编辑后点击 **下载 → 重新加载配置**（或 `POST /api/downloads/reload`）使客户端重新读取。

完整配置模板请参见 [`config.example.yaml`](config.example.yaml)。

---

## 项目结构

```
MediaStationGo/
├── cmd/server/main.go          应用入口
├── internal/
│   ├── config/                 Viper 配置加载
│   ├── database/               GORM + SQLite (WAL) 初始化
│   ├── model/                  GORM 数据模型 + AutoMigrate 注册
│   ├── repository/             数据访问层
│   ├── service/                业务逻辑
│   │   ├── auth.go             登录 / 注册 / JWT / 管理员种子
│   │   ├── media.go            媒体库 + 媒体 CRUD
│   │   ├── scanner.go          文件扫描 + ffprobe + 刮削触发
│   │   ├── ffprobe.go          ffprobe 封装
│   │   ├── tmdb.go             TMDb 数据源
│   │   ├── bangumi.go          Bangumi 数据源
│   │   ├── scraper.go          刮削协调器 + 文件名清洗
│   │   ├── site.go             站点管理（CRUD + 连接测试 + 跨站搜索）
│   │   ├── site_adapter.go     6 种 PT 站点适配器
│   │   ├── stream.go           直链播放 + HLS 分片
│   │   ├── transcoder.go       媒体 HLS 转码管理
│   │   ├── subtitle.go         外挂字幕识别 + WebVTT 转换
│   │   ├── image_proxy.go      图片代理缓存
│   │   ├── playback.go         播放历史 / 收藏 / 播放列表
│   │   ├── watcher.go          fsnotify 文件监听
│   │   ├── qbittorrent.go      qBittorrent API 客户端
│   │   ├── downloads.go        下载管理 + WS 轮询
│   │   ├── subscription.go     RSS 订阅轮询
│   │   ├── stats.go            仪表盘快照
│   │   ├── profile.go          用户信息修改
│   │   ├── audit.go            审计日志
│   │   ├── ws_hub.go           WebSocket 发布/订阅
│   │   ├── organizer.go        媒体文件整理
│   │   └── walk.go / episode_parser.go  辅助工具
│   ├── middleware/             Gin 中间件 (CORS / JWT / admin)
│   └── handler/                HTTP 路由（按功能分文件）
├── web/                        React 18 + Vite 前端
│   ├── src/api/                axios 接口封装
│   ├── src/components/         Layout / MediaCard / GlobalEvents / RequireAuth / APIConfigsPanel
│   ├── src/hooks/              useWebSocket 等
│   ├── src/pages/              首页 / 媒体库 / 搜索 / 播放器 / 下载 / 管理后台 / 站点管理
│   ├── src/stores/             Zustand 状态管理 (auth)
│   └── src/types/              前端类型定义（与 Go 模型对齐）
├── Dockerfile                  多阶段、多架构构建
├── docker-compose.yml          NAS 友好部署配置
├── Makefile                    build / dev / docker / test
├── config.example.yaml         完整配置模板
└── .github/workflows/          CI + GHCR 发布
```

---

## 许可证

基于 [GNU GPL v3.0](LICENSE) 开源发布。
