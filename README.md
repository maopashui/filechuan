# 飞船（File Transfer Station）

简洁、可爱、易部署的文件中转站：支持局域网 WebRTC 快传与管理员互联网文件中转。

## 功能

- **局域网快传**：无需登录，浏览器 P2P（WebRTC DataChannel），服务端只做房间与信令
- **互联网中转**：管理员登录后上传，生成取件码/链接；取件人凭码下载
- **单二进制部署**：React 构建产物经 Go `embed` 打进可执行文件
- **本地存储**：SQLite + 本地文件目录，无需外部数据库

## 环境要求

- Go 1.22+
- Node.js 20+ / npm（仅开发与构建前端需要；生产运行不需要）

## 快速开始（开发）

```bash
# 终端 1：后端
cp .env.example .env
make dev-api

# 终端 2：前端（Vite 代理到 :8080）
make dev-web
```

浏览器打开 http://127.0.0.1:5173

首次访问若尚未初始化，请打开「首次初始化管理员」创建唯一管理员账号。

## 构建单二进制

```bash
make build
# 产物：bin/filechuan
```

`make build` 会先构建前端并复制到 `webembed/dist`，再编译 Go。若仍是占位前端，构建会失败。

## 运行（生产）

```bash
export DATA_DIR=./data
export LISTEN_ADDR=:8080
# 若经 HTTPS 反代：COOKIE_SECURE=true
./bin/filechuan
```

- 首页：`http://<host>:8080/`
- 健康检查：`GET /healthz`
- 数据目录默认 `./data`（`app.db`、`files/`、`temp/`、`logs/`）

### 首次创建管理员

1. 启动服务后访问首页或 `/setup`
2. 当 `GET /api/v1/setup/status` 返回 `setup_needed: true` 时可注册
3. 注册成功后公开注册关闭；重启后仍保持关闭

重置管理员（运维命令，无公开 API）：

```bash
./bin/filechuan reset-admin
```

## 配置

参见 [.env.example](.env.example)。常用项：

| 变量 | 说明 | 默认 |
|------|------|------|
| `LISTEN_ADDR` | 监听地址 | `:8080` |
| `DATA_DIR` | 数据目录 | `./data` |
| `MAX_FILE_SIZE` | 单文件上限（字节） | 2GiB |
| `PART_SIZE` | 分片大小 | 5MiB |
| `SESSION_TTL` | 会话有效期 | `168h` |
| `COOKIE_SECURE` | HTTPS（Caddy 反代）时设为 true | `false` |
| `TRUSTED_PROXIES` | 信任的反代 IP/CIDR，用于正确识别 ClientIP | `127.0.0.1,::1` |
| `ROOM_MAX_AGE` | 房间自创建起最长保留 | `24h` |
| `ROOM_IDLE_TIMEOUT` | 空闲房间清理阈值 | `30m` |
| `STUN_SERVERS` | STUN 列表（逗号分隔） | Google 公共 STUN |
| `TURN_URL` / `TURN_USERNAME` / `TURN_CREDENTIAL` | 可选 TURN（会下发给浏览器 ICE） | 空 |

## HTTPS / STUN / TURN

- 生产建议用 Caddy/Nginx 终止 TLS，应用只监听本机；并设置 `COOKIE_SECURE=true`、`TRUSTED_PROXIES` 为反代地址
- 局域网房间须先通过 `POST /api/v1/rooms` 创建，最长保留 `ROOM_MAX_AGE`（默认 24h）
- 局域网 WebRTC 依赖 STUN；复杂 NAT 可能直连失败
- 首版**不强制**自建 TURN；配置静态 TURN 凭证后会下发给前端（浏览器 ICE 所需），公网请谨慎
- 直连失败时前端会明确提示，**不会**把文件偷偷上传到服务器

## 备份与恢复

停止服务后备份整个数据目录即可：

```bash
tar -czf filechuan-backup.tgz data/
```

恢复：

```bash
tar -xzf filechuan-backup.tgz
# 确保 DATA_DIR 指向恢复后的 data 目录后启动
```

同时包含 `app.db` 与 `files/`，二者需成对恢复。

## 日志

默认写入 `$DATA_DIR/logs/app.log`（JSON）。关键请求（`/api`、`/ws`、`/healthz`）会记录方法、路径、状态码与耗时。不会记录密码、Cookie 或明文取件码。

## 升级

1. 备份 `data/`
2. `make build` 得到新二进制
3. 停旧进程，替换二进制后启动
4. 数据库迁移在启动时自动执行

## 验证部署

```bash
./bin/filechuan &
curl -s http://127.0.0.1:8080/healthz
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/lan/demo
curl -s http://127.0.0.1:8080/api/v1/does-not-exist
# 期望：API 404 为 JSON，而非 HTML
```

重启后检查 `data/` 中数据库与已上传文件是否仍在。

## 测试

```bash
make test
# 或
go test ./...
cd web && npm test -- --run
```

## 项目结构

```
web/                 React + Vite + Ant Design
internal/            Go 业务模块（auth/file/pickup/signaling/...）
webembed/            嵌入的前端 dist
main.go              入口（含 reset-admin）
Makefile
```

## 已知限制与后续优化

- WebRTC 直连在部分 NAT/防火墙下会失败；未配置 TURN 时无法保证连通
- 当前环境若未做双浏览器真实互传，请在上线前自行用两台设备验证局域网快传
- 首版无多用户、无云存储、无 mDNS 自动发现、无强制下载密码
- 单机 SQLite，适合轻量自托管；超大并发需另行评估

## License

MIT
