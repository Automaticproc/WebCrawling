# 目录采集台

本地使用的多平台作品目录管理后台。后端是 Go，前端是 Vue 3，数据放在 MySQL 8。界面为简体中文。所有展示时间和定时任务都使用 **Asia/Hong_Kong**。

第一期包含：

- 七张业务表的迁移：`users`、`platforms`、`works`、`episodes`、`sync_jobs`、`sync_job_items`、`skip_rules`
- 必须登录的管理后台
- 多平台的增删改查
- 可配置跳过规则；被跳过的条目保留标题和原因
- 不可关闭的内置规则：未成年或未满 21 岁相关内容
- 简介只在跳过检查通过之后写入；命中未成年规则时不保存简介，并删除已有简介
- 播放、点赞、收藏、评论只保留最新快照
- 同步任务明细
- 每天 08:00（香港时间）的增量调度，以及手动全量 / 增量
- 同一平台已有进行中的同步时，新任务记为跳过
- `huangguoai` 适配器：只使用公开 sitemap 和作品页 HTML

播放链接只保留官方页面，形如 `/video/12/` 和 `/video/12/ep-2/`。系统不采集、不解密、不保存、不记录视频流、密钥、预览流或视频文件，也不会请求来源站的 `/api/`。

## 目录

```text
backend/                 Go API、迁移、同步与测试
  cmd/server/            进程入口
  migrations/            MySQL 迁移（另有 schema_migrations 记账表）
  internal/adapter/      请求策略、播放链接过滤、huangguoai 解析
  internal/skip/         跳过规则，含不可关闭的未成年规则
  internal/syncer/       同步执行
  internal/scheduler/    香港时间调度
frontend/                Vue 3 管理界面
docker-compose.yml       MySQL 8 + API + 前端
.env.example             本地配置样例
```

## 本地运行

需要 Go 1.22、Node.js 22 和 MySQL 8。

```bash
mysql -e "CREATE DATABASE IF NOT EXISTS catalog CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"
cp .env.example .env
# 修改 ADMIN_PASSWORD 和 SESSION_SECRET

set -a && source .env && set +a
cd backend && go run ./cmd/server
```

另一个终端：

```bash
cd frontend && npm install && npm run dev
```

打开 [http://127.0.0.1:43124](http://127.0.0.1:43124)，使用 `.env` 里的管理员账号登录。API 监听 `:43123`，开发服务器把 `/api` 代理过去。

启动时只会建库、创建管理员、写入内置跳过规则和 `huangguoai` 平台，**不会**自动抓取来源站。定时任务要等到下一次香港时间 08:00 才跑增量。

## Docker Compose

```bash
docker compose up --build
```

- 页面：<http://127.0.0.1:43124>
- API：<http://127.0.0.1:43123/api/v1/health>
- 默认管理员：`admin` / `change-me-please`（请在 `docker-compose.yml` 里改掉）
- MySQL：`127.0.0.1:3306`，库 `catalog`，用户 `catalog` / `catalog`

容器里的 API 使用主机名 `mysql`。本机直接 `go run` 时使用 `.env.example` 里的 `127.0.0.1`。

## 测试与构建

```bash
cd backend && go test ./...
cd frontend && npm install && npm run build
```

Go 测试使用假 HTML / sitemap，不访问线上站点。若本机 MySQL 可用，`mysqlstore` 测试会在 `catalog_test` 上跑迁移和读写；连不上则跳过，其余测试仍然执行。

## 同步行为

| 项目 | 行为 |
| --- | --- |
| 时区 | 调度、来源页上的无时区时间，都按香港时间解释；库存 UTC |
| 默认定时 | `0 8 * * *`，每天 08:00 增量 |
| 手动 | 平台页可发起增量或全量。全量会先确认 |
| 重叠 | 同一平台已有 `running` 任务时，新任务状态为 `skipped` |
| 增量窗口 | 有上次成功时间时，收录该香港日历日及之后 `lastmod` 的作品；从未同步过时，只看最近 24 小时所在日期及之后，避免第一次增量变成全站抓取 |
| 缺席次数 | 只在全量同步里，对这次站点地图没有出现的本地作品 +1；再次出现则归零 |
| 空站点地图 | 全量若没有任何作品地址，任务失败，不改缺席次数 |
| 指标 | `works` 上的播放、点赞、收藏、评论被覆盖为最新值，没有历史表 |
| 重启 | 上次进程留下的 `running` 任务会标成失败，避免平台被永久锁住 |

`huangguoai` 以外的平台可以保存，但同步会失败并说明暂无适配器。

## 采集边界

适配器只做这些请求：

- 平台配置的 sitemap 索引（默认 `/sitemap.xml`）
- `/sitemap/video/{n}.xml` 里的作品页地址和 `lastmod`
- `/video/{id}/` 作品页 HTML

来源站 `robots.txt` 禁止 `/api/`，客户端也会拒绝 `/api/`、后台、上传、搜索、登录注册、带查询串的地址、嵌入页和音视频扩展名。播放链接再过滤一次，只留下 `/video/{id}/` 与 `/video/{id}/ep-{n}/`（`ep-1` 规范成作品页）。封面只接受没有查询串的图片地址，并且不会下载文件。

简介先放在内存里做跳过判断。通过之后才写入 `works.description`。命中未成年规则时删除已有作品行，任务明细只记标题和原因，播放链接也不留。

## 已做决定

- 迁移执行器额外使用 `schema_migrations`，不计入那七张业务表。会话是 HMAC Cookie，因此没有第八张业务表。
- 作品发现不用带播放器信息的 video sitemap，只用 `/sitemap/video/{n}.xml` 的页面地址，再抓作品页。这样简介和页面链接都在跳过检查之后才决定是否落库。
- 第 1 集的官方地址是 `/video/{id}/`。`/video/{id}/ep-1/` 会回到这一地址，所以不单独保存。
- 点赞和收藏优先用页面上的整数；播放量由展示用的 `w` / `k` / `万` 文本换算。每次同步覆盖这四个数。
- 停用平台只停止定时任务，手动同步仍然可以发起。
- 测试和首次启动都不抓线上站点。适配器测试使用 `httptest` 和 `testdata` 里的虚构页面。
