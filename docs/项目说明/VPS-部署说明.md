# Multica 单台 VPS 部署说明

本文面向一台普通 Linux VPS 的自托管部署：同一台机器运行 PostgreSQL、Multica Backend、Multica Web，并通过 Caddy 或 Nginx 暴露 HTTPS 访问。

如果部署的是当前公司 CAS SSO 改造版本，优先使用“从当前代码构建镜像”的方式，而不是直接拉官方 `latest` 镜像。官方镜像可能不包含公司定制的 CAS / 免邮件邀请逻辑。

---

## 1. 推荐部署形态

### 1.1 单域名部署

推荐先用一个域名：

```txt
https://multica.example.com
```

访问路径：

| 路径 | 说明 |
|---|---|
| `/` | Web 前端 |
| `/api/*` | 由 Next.js rewrite 转发到后端 |
| `/auth/*` | CAS callback / 登录相关接口，转发到后端 |
| `/ws` | WebSocket，转发到后端 |
| `/uploads/*` | 本地上传文件，转发到后端 |

这种方式最简单，`COOKIE_DOMAIN` 可以留空，不需要处理跨子域 cookie。

### 1.2 双域名部署（可选）

如果后续要拆分前后端域名，可以使用：

```txt
https://multica.example.com      # Web
https://multica-api.example.com  # Backend
```

双域名模式需要额外确认 CORS、cookie domain、CLI server URL，第一版部署不建议先走这个方案。

---

## 2. VPS 前置准备

### 2.1 最低规格建议

| 资源 | 建议 |
|---|---|
| CPU | 2 核及以上 |
| 内存 | 4 GB 起步，8 GB 更稳 |
| 磁盘 | 40 GB 起步，建议 SSD |
| 系统 | Ubuntu 22.04 / 24.04 LTS |

如果多人同时使用本地 agent、上传附件较多或 issue 数据增长较快，建议单独规划数据库备份和磁盘监控。

### 2.2 域名和端口

准备一个域名并解析到 VPS 公网 IP：

```txt
multica.example.com -> VPS 公网 IP
```

安全组 / 防火墙只需要对公网开放：

```txt
22/tcp   SSH
80/tcp   HTTP，用于申请证书和自动跳转 HTTPS
443/tcp  HTTPS
```

`3000` 和 `8080` 是容器内部服务端口，不建议直接对公网开放。实际访问应走反向代理的 `443`。

---

## 3. 安装基础依赖

以 Ubuntu 为例：

```bash
sudo apt update
sudo apt install -y ca-certificates curl git gnupg make openssl
```

安装 Docker：

```bash
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker "$USER"
```

重新登录 SSH，让当前用户获得 Docker 权限，然后验证：

```bash
docker version
docker compose version
```

安装 Caddy：

```bash
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf "https://dl.cloudsmith.io/public/caddy/stable/gpg.key" | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf "https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt" | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update
sudo apt install -y caddy
```

---

## 4. 拉取代码

如果使用公司 fork：

```bash
git clone <your-company-multica-repo-url> /opt/multica
cd /opt/multica
```

如果已经 fork 到自己的仓库，也可以替换为你的仓库地址。公司 CAS 改造建议固定使用包含改造代码的分支，例如：

```bash
git checkout company/cas-sso
```

---

## 5. 配置 `.env`

复制示例文件：

```bash
cp .env.example .env
```

生成 JWT secret：

```bash
openssl rand -hex 32
```

编辑 `.env`：

```bash
nano .env
```

### 5.1 基础生产配置

示例：

```env
APP_ENV=production

POSTGRES_DB=multica
POSTGRES_USER=multica
POSTGRES_PASSWORD=<换成强密码>
POSTGRES_PORT=5432

JWT_SECRET=<openssl rand -hex 32 生成的值>

PORT=8080
FRONTEND_PORT=3000
FRONTEND_ORIGIN=https://multica.example.com
MULTICA_APP_URL=https://multica.example.com
MULTICA_SERVER_URL=wss://multica.example.com/ws

COOKIE_DOMAIN=
```

说明：

1. `JWT_SECRET` 必须修改，不能使用默认值；
2. 单域名部署时 `COOKIE_DOMAIN` 留空；
3. `FRONTEND_ORIGIN` 必须是浏览器访问 Multica 的 HTTPS 地址；
4. `MULTICA_SERVER_URL` 给 CLI / daemon 使用，HTTPS 页面对应 `wss://.../ws`。

### 5.2 CAS SSO 配置

公司 CAS 部署建议关闭邮箱验证码和 Google 登录，只保留公司 SSO：

```env
CAS_ENABLED=true
CAS_DISPLAY_NAME="米可世界统一飞书登录"

CAS_LOGIN_URL=https://<公司CAS地址>/cas/login
CAS_VALIDATE_URL=https://<公司CAS地址>/cas/serviceValidate
CAS_SERVICE_URL=https://multica.example.com/auth/cas/callback

CAS_ATTRIBUTE_EMAIL=user
CAS_ATTRIBUTE_NAME=displayName
CAS_ATTRIBUTE_AVATAR=avatarOrigin
CAS_EMAIL_DOMAIN=

EMAIL_LOGIN_ENABLED=false
GOOGLE_LOGIN_ENABLED=false
```

注意：

1. `CAS_SERVICE_URL` 是 Multica 的回调地址，不是 CAS 服务端地址；
2. 这个地址必须能被用户浏览器访问，也必须在 CAS 平台登记为允许回调地址；
3. 如果 CAS 的 `<cas:user>` 本身就是邮箱，使用 `CAS_ATTRIBUTE_EMAIL=user`；
4. 如果 `<cas:user>` 不是邮箱，需要让 CAS 返回邮箱字段，或配置 `CAS_EMAIL_DOMAIN=company.com`。

### 5.3 免邮件邀请自动加入

如果不配置邮件服务，可以用“预邀请 + SSO 登录后自动加入”：

```env
INVITATION_EMAIL_ENABLED=false
INVITATION_AUTO_ACCEPT_ON_LOGIN=true
```

效果：

1. 管理员在成员页输入邮箱后，系统只保存 pending invitation；
2. 不发送邀请邮件；
3. 用户之后用同一个邮箱通过公司 SSO 登录；
4. 登录成功后自动加入对应工作区。

如果后续要启用真实邮件邀请，再改为：

```env
INVITATION_EMAIL_ENABLED=true
INVITATION_AUTO_ACCEPT_ON_LOGIN=false
```

并配置 `RESEND_API_KEY` 或 `SMTP_HOST`。

---

## 6. 启动服务

### 6.1 推荐：从当前代码构建

公司 CAS 改造分支建议使用：

```bash
make selfhost-build
```

这个命令会：

1. 使用 Docker Compose 启动 PostgreSQL、Backend、Frontend；
2. 从当前 checkout 构建 `multica-backend:dev` 和 `multica-web:dev`；
3. Backend 启动时自动执行数据库迁移；
4. 服务异常退出后由 Docker 自动重启。

查看服务状态：

```bash
docker compose -f docker-compose.selfhost.yml ps
```

查看日志：

```bash
docker compose -f docker-compose.selfhost.yml logs -f backend
docker compose -f docker-compose.selfhost.yml logs -f frontend
```

### 6.2 使用官方镜像（仅适合无公司定制时）

如果未来公司改造已经发布成内部镜像，或者使用官方原版：

```bash
make selfhost
```

也可以固定镜像版本：

```env
MULTICA_IMAGE_TAG=v0.x.x
```

### 6.3 可选：裸源码部署

如果不想用 Docker 跑应用，也可以在 VPS 上直接从源码构建并用 `systemd` 托管。

先明确两种“源码启动”的区别：

| 方式 | 命令 | 用途 |
|---|---|---|
| 开发启动 | `make start` | 本地开发，后端 `go run`，前端 `next dev`，不适合生产 |
| 生产源码部署 | `make build` + `pnpm build` + `systemd` | VPS 上从源码构建产物，再长期运行 |

生产环境不要直接用 `make start` 挂在 SSH 里跑。SSH 断开、进程退出、机器重启都会影响服务。

#### 6.3.1 安装源码构建依赖

裸源码部署需要：

| 依赖 | 建议版本 |
|---|---|
| Go | 1.26+ |
| Node.js | 22 |
| pnpm | 10.28+ |
| PostgreSQL | 17 + pgvector |

Node.js 和 pnpm 示例：

```bash
curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash -
sudo apt install -y nodejs

corepack enable
corepack prepare pnpm@10.28.2 --activate

node -v
pnpm -v
```

Go 可以使用官方二进制、系统包或内部基础镜像安装。安装后确认：

```bash
go version
```

PostgreSQL 可以独立安装，也可以只用 Docker 跑数据库。最省事的数据库方式：

```bash
docker run -d \
  --name multica-postgres \
  --restart unless-stopped \
  -e POSTGRES_DB=multica \
  -e POSTGRES_USER=multica \
  -e POSTGRES_PASSWORD=<换成强密码> \
  -p 127.0.0.1:5432:5432 \
  -v multica_pgdata:/var/lib/postgresql/data \
  pgvector/pgvector:pg17
```

对应 `.env`：

```env
DATABASE_URL=postgres://multica:<换成强密码>@localhost:5432/multica?sslmode=disable
```

#### 6.3.2 构建后端和前端

在代码目录执行：

```bash
cd /opt/multica

pnpm install

# 构建 Go 后端、CLI 和迁移工具
make build

# 构建 Next.js 前端
REMOTE_API_URL=http://localhost:8080 pnpm --filter @multica/web build
```

构建完成后，关键产物是：

```txt
server/bin/server    # 后端 API / WebSocket 服务
server/bin/migrate   # 数据库迁移工具
apps/web/.next/      # Next.js 生产构建产物
```

每次拉取新代码后，都需要重新执行构建命令。源码改动不会自动影响已运行的生产进程。

#### 6.3.3 执行数据库迁移

```bash
cd /opt/multica
set -a
. ./.env
set +a

cd server
./bin/migrate up
```

迁移需要能读取 `DATABASE_URL`。如果使用 `systemd` 托管，服务启动前也建议先手动执行一次迁移。

#### 6.3.4 手动启动验证

先开一个终端启动后端：

```bash
cd /opt/multica
set -a
. ./.env
set +a

cd server
./bin/server
```

再开另一个终端启动前端：

```bash
cd /opt/multica
set -a
. ./.env
set +a

PORT=3000 REMOTE_API_URL=http://localhost:8080 pnpm --filter @multica/web start
```

验证：

```bash
curl -i http://localhost:8080/health
curl -i http://localhost:3000/api/config
```

确认没问题后，再改成 `systemd` 长期运行。

#### 6.3.5 使用 systemd 托管后端

创建服务文件：

```bash
sudo nano /etc/systemd/system/multica-backend.service
```

写入：

```ini
[Unit]
Description=Multica Backend
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/multica/server
EnvironmentFile=/opt/multica/.env
ExecStart=/opt/multica/server/bin/server
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

#### 6.3.6 使用 systemd 托管前端

创建服务文件：

```bash
sudo nano /etc/systemd/system/multica-web.service
```

写入：

```ini
[Unit]
Description=Multica Web
After=network.target multica-backend.service

[Service]
Type=simple
WorkingDirectory=/opt/multica
EnvironmentFile=/opt/multica/.env
Environment=PORT=3000
Environment=REMOTE_API_URL=http://localhost:8080
ExecStart=/usr/bin/env pnpm --filter @multica/web start
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

注意：`.env` 里 `PORT=8080` 是后端端口；前端服务必须额外覆盖为 `PORT=3000`，否则 `next start` 可能会占用后端端口。

启动服务：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now multica-backend
sudo systemctl enable --now multica-web
```

查看状态和日志：

```bash
systemctl status multica-backend
systemctl status multica-web

journalctl -u multica-backend -f
journalctl -u multica-web -f
```

源码部署时，Caddy 配置仍然使用下一节的 `localhost:3000` 和 `localhost:8080`。

---

## 7. 配置反向代理

### 7.1 Caddy 推荐配置

编辑：

```bash
sudo nano /etc/caddy/Caddyfile
```

写入：

```caddy
multica.example.com {
    encode zstd gzip

    @multica_ws path /ws /ws/*
    handle @multica_ws {
        reverse_proxy localhost:8080 {
            flush_interval -1
        }
    }

    reverse_proxy localhost:3000
}
```

加载配置：

```bash
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

Caddy 会自动申请和续期 HTTPS 证书。确认域名已经解析到 VPS，并且 80 / 443 没有被安全组拦截。

### 7.2 为什么只代理 `/ws` 到后端

在单域名部署下，浏览器访问 `https://multica.example.com` 会先进入 Next.js 前端。前端自身配置了 rewrite，会把这些路径转发到 backend：

```txt
/api/*
/auth/*
/ws
/uploads/*
```

所以 Caddy 只需要特别处理 WebSocket 的 `/ws`，其他请求交给前端即可。

---

## 8. 验证部署

### 8.1 健康检查

```bash
curl -i http://localhost:8080/health
curl -i https://multica.example.com/api/config
```

`/api/config` 里应能看到：

```json
{
  "auth": {
    "email_login_enabled": false,
    "google_login_enabled": false,
    "cas": {
      "enabled": true,
      "display_name": "米可世界统一飞书登录",
      "login_url": "https://multica.example.com/auth/cas/start"
    }
  }
}
```

### 8.2 登录页检查

打开：

```txt
https://multica.example.com/login
```

预期：

1. 看到“公司 SSO”登录入口；
2. 不展示邮箱验证码登录；
3. 不展示 Google 登录；
4. 点击后跳转到公司 CAS；
5. CAS 登录成功后回到 Multica。

### 8.3 邀请自动加入检查

1. 管理员进入某个工作区；
2. 打开 `Settings -> Members`；
3. 输入员工邮箱并邀请；
4. 让该员工用同邮箱 SSO 登录；
5. 登录后应自动出现在该工作区成员列表。

---

## 9. CLI / Daemon 接入

每个需要运行本地 agent 的成员，在自己的电脑上安装 CLI：

```bash
brew install multica-ai/tap/multica
```

连接自托管服务：

```bash
multica setup self-host \
  --server-url https://multica.example.com \
  --app-url https://multica.example.com
```

验证：

```bash
multica auth status
multica daemon status
```

如果使用 Linux 客户端且没有 Homebrew，需要按官方 CLI 安装方式或内部发布方式安装 `multica` 二进制。

---

## 10. 日常运维

### 10.1 启停

```bash
# 启动 / 更新当前构建
docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build

# 停止服务
make selfhost-stop
```

### 10.2 查看日志

```bash
docker compose -f docker-compose.selfhost.yml logs -f backend
docker compose -f docker-compose.selfhost.yml logs -f frontend
docker compose -f docker-compose.selfhost.yml logs -f postgres
```

### 10.3 备份数据库

建议至少每日备份 PostgreSQL：

```bash
mkdir -p /opt/multica/backups

docker compose -f docker-compose.selfhost.yml exec -T postgres \
  pg_dump -U multica -d multica \
  > /opt/multica/backups/multica-$(date +%F-%H%M%S).sql
```

恢复示例：

```bash
docker compose -f docker-compose.selfhost.yml exec -T postgres \
  psql -U multica -d multica \
  < /opt/multica/backups/<backup-file>.sql
```

恢复前建议先停 backend，避免写入冲突。

### 10.4 升级

公司 fork 分支升级建议：

```bash
cd /opt/multica
git fetch
git pull --ff-only
make selfhost-build
```

Backend 容器启动时会自动执行迁移。升级前先做数据库备份。

如果使用裸源码 + `systemd` 部署，升级流程改为：

```bash
cd /opt/multica
git fetch
git pull --ff-only

pnpm install
make build
REMOTE_API_URL=http://localhost:8080 pnpm --filter @multica/web build

set -a
. ./.env
set +a
cd server
./bin/migrate up

sudo systemctl restart multica-backend
sudo systemctl restart multica-web
```

源码部署不会自动消费最新源码；必须重新构建后再重启服务。

---

## 11. 常见问题

### 11.1 登录页没有出现公司 SSO

检查：

```bash
docker compose -f docker-compose.selfhost.yml exec backend env | grep CAS
curl -s https://multica.example.com/api/config
```

确认：

1. `CAS_ENABLED=true`；
2. `CAS_LOGIN_URL` 不为空；
3. `CAS_VALIDATE_URL` 不为空；
4. `CAS_SERVICE_URL` 是公网 HTTPS callback；
5. 修改 `.env` 后已经重启 compose。

### 11.2 CAS 登录后回调失败

重点检查：

1. CAS 平台登记的 service/callback 是否等于 `CAS_SERVICE_URL`；
2. `CAS_SERVICE_URL` 是否为 `https://multica.example.com/auth/cas/callback`；
3. Caddy 是否正常把请求转发到前端，再由 Next.js rewrite 转发到 backend；
4. backend 日志里是否有 CAS ticket validate 错误。

查看 backend 日志：

```bash
docker compose -f docker-compose.selfhost.yml logs -f backend
```

### 11.3 邀请后用户没有自动加入

确认：

1. `INVITATION_AUTO_ACCEPT_ON_LOGIN=true`；
2. `INVITATION_EMAIL_ENABLED=false` 只是关闭邮件，不影响 pending invitation 创建；
3. 邀请邮箱和 SSO 返回邮箱完全一致；
4. 用户已经重新登录过一次；
5. 该邀请仍是 pending 状态。

### 11.4 WebSocket 或实时更新异常

检查 Caddy 是否包含：

```caddy
@multica_ws path /ws /ws/*
handle @multica_ws {
    reverse_proxy localhost:8080 {
        flush_interval -1
    }
}
```

然后重载：

```bash
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

---

## 12. 上线检查清单

上线前确认：

1. VPS 安全组只开放 `22`、`80`、`443`；
2. DNS 已解析到 VPS；
3. HTTPS 证书正常；
4. `.env` 中 `JWT_SECRET` 和 `POSTGRES_PASSWORD` 已更换；
5. `FRONTEND_ORIGIN=https://multica.example.com`；
6. `CAS_SERVICE_URL=https://multica.example.com/auth/cas/callback`；
7. CAS 平台允许该 callback；
8. `/api/config` 显示 CAS 已启用；
9. 管理员可以 SSO 登录；
10. 邀请用户后，用户 SSO 登录可以自动加入工作区；
11. 已配置数据库备份；
12. 已记录升级方式和回滚方式。
