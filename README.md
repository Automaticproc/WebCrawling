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
- 按作品批量下载：逐集解析播放页 → 抓 m3u8（可选无头浏览器）→ 下载分片 → AES-128 解密 → 合并 MP4

播放链接只保留官方页面，形如 `/video/12/` 和 `/video/12/ep-2/`。同步流程不采集、不解密、不保存视频流；下载是**单独的可选动作**，只有你点击「下载全部集」才会去解析播放地址并保存文件，而且只应下载你确认有权保存的内容。

## 目录

```text
backend/                 Go API、迁移、同步与测试
  cmd/server/            进程入口
  migrations/            MySQL 迁移（另有 schema_migrations 记账表）
  internal/adapter/      请求策略、播放链接过滤、huangguoai 解析
  internal/skip/         跳过规则，含不可关闭的未成年规则
  internal/syncer/       同步执行
  internal/scheduler/    香港时间调度
  internal/download/     HLS 解析 / 并发下载 / AES-128 解密 / 浏览器抓流 / ffmpeg 合封
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

## 下载（可选）

作品详情页的「下载全部集」会为每一集建一条下载明细：

1. 取该集的官方页面地址（`/video/{id}/`、`/video/{id}/ep-{n}/`）；
2. 解析播放地址：
   - 先抓页面 HTML / 内嵌 JSON 里的 `m3u8`；
   - HTML 里没有时，启动无头 Chrome/Edge（`--headless=new`），用 CDP 监听网络请求，拿播放器真实请求的 m3u8，并复用它的 `Cookie` 与请求头（`Referer`、`x-cdn-auth` 等）；
   - 需要登录才能看的页面，可以配置自动登录，或直接复用已有登录态（见下）。
3. 纯 Go 下载 HLS：解析 master / media 播放列表，支持 `EXT-X-KEY`（AES-128 CBC）、`EXT-X-MAP`、`EXT-X-BYTERANGE`，按并发下载分片；
4. `ffmpeg -c copy` 无损合封为 MP4，写入 `DOWNLOAD_DIR/<平台代码>/<作品标题>/第NN集.mp4`；
5. 前端「下载任务」页可以看到逐集状态、分片进度、失败原因，并直接下载已完成的 MP4。

配置（`.env` / compose；登录相关项是全局兜底，平台管理里的配置优先）：

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `DOWNLOAD_DIR` | `downloads` | 输出根目录，也是文件下载接口允许的根目录 |
| `FFMPEG_PATH` | 空 | 留空从 `PATH` 查找 |
| `DOWNLOAD_BROWSER` | 空 | Chrome/Edge/Chromium 路径；留空自动探测。容器里指向 `/usr/bin/chromium` |
| `DOWNLOAD_BROWSER_PORT` | 空 | 连接已开着的 Chrome 调试端口（如 `9222`），复用登录态，不再新起浏览器 |
| `DOWNLOAD_BROWSER_PROFILE` | `<DOWNLOAD_DIR>/.browser-profile` | 浏览器用户目录；保存 Cookie / 登录态，跨进程复用 |
| `DOWNLOAD_COOKIE` | 空 | 直接注入 Cookie，如 `vip=1; token=abc`，适合已有登录态 |
| `DOWNLOAD_LOGIN_URL` | 空 | 登录页地址；配置后抓流前会先自动登录 |
| `DOWNLOAD_USERNAME` / `DOWNLOAD_PASSWORD` | 空 | 自动登录用的账号密码 |
| `DOWNLOAD_LOGIN_USER_SELECTOR` 等三个 | 空 | 用户名 / 密码 / 提交按钮的 CSS 选择器，不填则自动猜 |
| `DOWNLOAD_BROWSER_TIMEOUT_SEC` | `45` | 单页抓流等待上限 |
| `DOWNLOAD_CONCURRENCY` | `8` | 单集分片并发数 |

登录态有三种用法，按侵入性从低到高：

1. **连接已登录的浏览器**：用 `--remote-debugging-port=9222` 启动 Chrome，登录好站点，再设 `DOWNLOAD_BROWSER_PORT=9222`。不存密码，最推荐。
2. **注入 Cookie**：从 F12 把 Cookie 复制到 `DOWNLOAD_COOKIE`。
3. **自动登录**：配置 `DOWNLOAD_LOGIN_URL` + 账号密码，第一次抓该站前会打开登录页、填表并提交；同一进程内后续集不再重复登录，配合持久化 profile 重启后也能复用。

自动登录的边界：表单登录可用；验证码、短信/扫码、2FA、iframe 登录、设备绑定都过不了，需要先用方式 1 或 2 把登录态准备好。密码以环境变量形式存在，注意宿主权限，只登录你自己的账号。

### 平台级登录配置（推荐）

上面的环境变量是全局兜底；日常更建议在「平台管理 → 编辑平台 → 登录配置」里按平台填：

| 字段 | 说明 |
| --- | --- |
| 登录页地址 | 该平台的登录页，例如 `https://site/login` |
| 登录账号 / 密码 | 自动填表用；密码用 `SESSION_SECRET` 派生的 AES-GCM 加密后落库，接口不回传明文 |
| 用户名 / 密码 / 提交按钮选择器 | 可选；不填按 `input[type=text]`、`input[type=password]`、`button[type=submit]` 猜 |
| Cookie | 可选，优先级高于自动登录；同样只显示“已保存”，不回传 |
| 浏览器 profile 目录 | 可选；留空默认 `<DOWNLOAD_DIR>/.browser-profile/<平台代码>`，每个平台 Cookie 隔离 |

注意：改 `SESSION_SECRET` 会导致已保存的登录密码无法解密，需要重新填一次。平台配置优先于全局环境变量。

说明：Windows 上如果 Chrome 以受限令牌启动（某些服务/容器/CI 环境），自带沙箱会直接崩，
工具会自动用 `--no-sandbox` 再启动一次；正常桌面环境不会走到这一步。

API：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/v1/works/{id}/downloads` | 下载整部作品，body `{"quality":"best"}` |
| GET | `/api/v1/downloads?work_id=&page=&page_size=` | 任务列表 |
| GET | `/api/v1/downloads/{id}` | 任务详情（含逐集明细与实时分片进度） |
| POST | `/api/v1/downloads/{id}/cancel` | 取消 |
| POST | `/api/v1/downloads/{id}/retry` | 只重试失败 / 取消 / 文件丢失的集 |
| GET | `/api/v1/downloads/{id}/items/{itemID}/file` | 下载已完成的 MP4 |

失败会留痕，也可以只补失败的部分：

- 任务行保留 `status=failed`、`failed_episodes`、`finished_at`，`error_message` 带第一处失败原因；
- 逐集明细保留 `status=failed`、`error_message`（如 `HTTP 403`）和 `page_url`，在任务详情里能直接看到是哪一集、为什么失败；
- 「重试失败集」会新建一个任务，只包含失败的、取消时还没跑的、以及文件已经不在的集；已完成且文件还在的集不会重复下载；
- 重试会重新请求播放页拿新签名，不复用旧 m3u8（分片地址带时效，过期后续传没有意义）。

实现细节：

- 同一部作品同时只允许一个未完成任务；进程重启后遗留的 `queued` / `running` 会在启动时标记为失败；
- 分片先落在 `DOWNLOAD_DIR/.tmp` 下的临时目录，完成后清理；
- 仓库自带 `internal/download` 的单元测试，会用 ffmpeg 现场生成一段 HLS 再下载合并；
- 有些站点的播放地址在加密接口里、或播放器用 `blob:` 直出，此时 HTML 解析无效，需要浏览器抓流；极少数站点还会有更严格的风控，抓不到时会返回明确错误。

## 站点地图怎么获取

平台管理里的「站点地图路径」是**相对源站的路径**，不是完整地址。

1. 先看 `https://站点/robots.txt`，找 `Sitemap:` 行；有多个就逐个试。
2. 直接打开 `https://站点/sitemap.xml`。如果它是索引（里面是 `<sitemap><loc>...`），适配器会自动继续读子地图。
3. 也可以从浏览器 F12 → Network 搜 `sitemap`，或看页面源码里的 `<link rel="sitemap">`。
4. 只填路径（`/sitemap.xml`），不要带域名、查询串，也不能是 `/api/` 下的地址。

以 `huangguoai` 为例：

- 源站地址：`https://huangguoai.com`
- 站点地图路径：`/sitemap.xml`
- 索引里的子地图：`/sitemap/video/{n}.xml`
- 子地图里的 `<loc>` 是作品页 `/video/{id}/`，`<lastmod>` 用于增量筛选
- 同步只用这些作品页 HTML，不请求来源站的 `/api/`

没有 sitemap（或 robots 禁止）的站点目前接不进来：采集依赖站点地图发现作品，不支持手工导入。

## 采集边界

适配器只做这些请求：

- 平台配置的 sitemap 索引（默认 `/sitemap.xml`）
- `/sitemap/video/{n}.xml` 里的作品页地址和 `lastmod`
- `/video/{id}/` 作品页 HTML

来源站 `robots.txt` 禁止 `/api/`，客户端也会拒绝 `/api/`、后台、上传、搜索、登录注册、带查询串的地址、嵌入页和音视频扩展名。播放链接再过滤一次，只留下 `/video/{id}/` 与 `/video/{id}/ep-{n}/`（`ep-1` 规范成作品页）。封面只接受没有查询串的图片地址，并且不会下载文件。

上面的策略只约束**采集适配器**。下载流程不请求 `/api/`，只请求播放页 HTML 和 CDN 的 m3u8 / 分片 / 密钥；它不参与页面发现与落库。

简介先放在内存里做跳过判断。通过之后才写入 `works.description`。命中未成年规则时删除已有作品行，任务明细只记标题和原因，播放链接也不留。

## 已做决定

- 迁移执行器额外使用 `schema_migrations`，不计入那七张业务表。会话是 HMAC Cookie，因此没有第八张业务表。
- 作品发现不用带播放器信息的 video sitemap，只用 `/sitemap/video/{n}.xml` 的页面地址，再抓作品页。这样简介和页面链接都在跳过检查之后才决定是否落库。
- 第 1 集的官方地址是 `/video/{id}/`。`/video/{id}/ep-1/` 会回到这一地址，所以不单独保存。
- 点赞和收藏优先用页面上的整数；播放量由展示用的 `w` / `k` / `万` 文本换算。每次同步覆盖这四个数。
- 停用平台只停止定时任务，手动同步仍然可以发起。
- 测试和首次启动都不抓线上站点。适配器测试使用 `httptest` 和 `testdata` 里的虚构页面。
