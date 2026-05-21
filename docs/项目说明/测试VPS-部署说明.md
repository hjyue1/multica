# Multica 测试 VPS 部署说明

更新时间：2026-05-21

本文面向当前线上测试环境：

```txt
https://multica.micoplatform.com
```

当前测试环境的部署原则：

1. 在测试 VPS 上从当前代码 checkout 构建本地 Docker 镜像；
2. 不走 ACR 推送 / 拉取流程；
3. 仓库根目录的 `.env.test` 就是测试 VPS 的 `.env` 来源；
4. 测试 VPS 上实际运行时使用仓库根目录 `.env` 文件；
5. Backend 连接 `.env` 里的远程 `DATABASE_URL`，不启动本地 PostgreSQL 容器。

通用 VPS 准备、Caddy 安装和单域名反向代理说明见 [VPS-部署说明.md](./VPS-部署说明.md)。本文只记录当前测试环境的固定部署口径。

---

## 1. 相关文件

| 文件 | 用途 |
|---|---|
| `.env.test` | 测试 VPS 环境变量来源 |
| `.env` | 测试 VPS 实际运行时读取的环境变量文件，不单独维护 |
| `docker-compose.selfhost.yml` | 自托管 Compose 配置：运行 Backend / Web，连接远程 PostgreSQL |
| `docker-compose.selfhost.build.yml` | 覆盖官方镜像，改为从当前代码构建本地镜像 |
| `Dockerfile` | Backend 镜像构建文件 |
| `Dockerfile.web` | Web 镜像构建文件 |

不要在测试 VPS 上长期手改 `.env` 而不同步仓库 `.env.test`。需要调整测试环境配置时，先更新 `.env.test`，再同步到 VPS 的 `.env`。

---

## 2. 首次部署

### 2.1 拉取代码

```bash
git clone <company-multica-repo-url> /opt/multica
cd /opt/multica
git checkout <测试环境部署分支>
```

如果测试 VPS 已经有代码目录：

```bash
cd /opt/multica
git fetch
git pull --ff-only
```

### 2.2 生成测试 VPS 的 `.env`

测试 VPS 的 `.env` 直接来自仓库 `.env.test`：

```bash
cd /opt/multica
cp .env.test .env
```

复制完成后，可以验证 Compose 展开的最终配置：

```bash
docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml config
```

标准运行方式始终是先复制为 `.env`，再执行 `make selfhost-build`。该目标内部会叠加 `docker-compose.selfhost.yml` 和 `docker-compose.selfhost.build.yml`。

### 2.3 构建本地镜像并启动

```bash
cd /opt/multica
make selfhost-build
```

该命令会：

1. 从当前 checkout 构建 Backend 镜像；
2. 从当前 checkout 构建 Web 镜像；
3. 把 `.env` 注入 Backend 容器；
4. 让 Backend 使用 `.env` 里的远程 `DATABASE_URL`；
5. 只启动 Backend / Web，不启动本地 PostgreSQL 容器。

启动后，本机 Docker 会生成并运行这些应用镜像：

```txt
multica-backend:dev
multica-web:dev
```

Backend 容器启动时会执行 `docker/entrypoint.sh`，先跑数据库迁移，再启动后端服务。

---

## 3. 日常更新

每次要把新代码部署到测试 VPS：

```bash
cd /opt/multica
git fetch
git pull --ff-only
cp .env.test .env
make selfhost-build
```

如果只改了 `.env.test`，也执行同一组命令。Compose 会按当前配置重建或重启需要变化的容器。

---

## 4. Compose 行为说明

### 4.1 为什么要复制 `.env.test` 为 `.env`

测试 VPS 标准命令默认读取项目根目录 `.env` 做变量替换，并通过 `env_file` 把 `.env` 注入 Backend 容器，因此测试 VPS 需要有：

```txt
/opt/multica/.env
```

团队约定是：

```bash
cp .env.test .env
```

这样可以让仓库 `.env.test` 成为测试环境配置的唯一来源。

### 4.2 本地构建镜像

`docker-compose.selfhost.build.yml` 会从当前 checkout 的 `Dockerfile`、`Dockerfile.web` 构建：

```txt
multica-backend:dev
multica-web:dev
```

### 4.3 数据库连接

当前测试 VPS 使用远程 PostgreSQL。Backend 容器实际使用 `.env` 里的：

```env
DATABASE_URL=postgres://hjyue:mdl123@proxy.liudododo.com:55432/testdb?sslmode=disable
```

`POSTGRES_DB`、`POSTGRES_USER`、`POSTGRES_PASSWORD`、`POSTGRES_PORT` 只保留为数据库信息记录和兼容脚本使用；测试 VPS 的应用容器不靠这些变量拼接连接串。

### 4.4 Web 代理到 Backend

`docker-compose.selfhost.build.yml` 构建 Web 镜像时传入：

```txt
REMOTE_API_URL=http://backend:8080
```

Next.js 会把以下路径转发到 Backend：

```txt
/api/*
/auth/*
/ws
/uploads/*
```

`.env.test` 里的 `NEXT_PUBLIC_WS_URL` 保持为空，浏览器会根据当前访问域名自动推导：

```txt
wss://multica.micoplatform.com/ws
```

---

## 5. Caddy 配置

测试环境单域名部署，Caddy 只需要把 `/ws` 特别代理到 Backend，其余流量给 Web：

```caddy
multica.micoplatform.com {
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

变更后验证并重载：

```bash
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

---

## 6. 验证

查看容器状态：

```bash
docker compose -f docker-compose.selfhost.yml ps
```

查看日志：

```bash
docker compose -f docker-compose.selfhost.yml logs -f backend
docker compose -f docker-compose.selfhost.yml logs -f frontend
```

本机健康检查：

```bash
curl -i http://localhost:8080/health
curl -i http://localhost:8080/readyz
```

公网检查：

```bash
curl -i https://multica.micoplatform.com/api/config
```

登录页检查：

```txt
https://multica.micoplatform.com/login
```

预期：

1. 显示公司 SSO 登录入口；
2. 不显示邮箱验证码登录；
3. 不显示 Google 登录；
4. 点击 SSO 后跳转到公司 CAS；
5. CAS 登录成功后回到 Multica。

---

## 7. 启停和回滚

停止服务：

```bash
cd /opt/multica
docker compose -f docker-compose.selfhost.yml down
```

回滚到上一个 Git 版本：

```bash
cd /opt/multica
git log --oneline -n 10
git checkout <previous-commit-or-tag>
cp .env.test .env
make selfhost-build
```

如果只是临时回滚测试环境，可以先不要提交或推送任何代码；确认原因后再决定是否在分支上 revert。

---

## 8. 数据和文件持久化

测试 VPS 的 PostgreSQL 数据在远程数据库，不在本机 Docker volume 里。

Compose 只使用 Docker volume 持久化后端本地上传文件：

| Volume | 内容 |
|---|---|
| `multica_backend_uploads` | 后端本地上传文件 |

普通停止命令不会删除 volume：

```bash
docker compose -f docker-compose.selfhost.yml down
```

不要在测试 VPS 上随意执行：

```bash
docker compose -f docker-compose.selfhost.yml down -v
```

`-v` 会删除上传文件 volume。数据库在远程 PostgreSQL，不会被这条 Compose 命令删除。

---

## 9. 备份

升级前建议按远程 PostgreSQL 的运维规范备份数据库。测试 VPS 本机如果没有安装 `pg_dump`，可以临时用 PostgreSQL 镜像执行备份：

```bash
cd /opt/multica
set -a
. ./.env
set +a

mkdir -p /opt/multica/backups

docker run --rm \
  -e DATABASE_URL="$DATABASE_URL" \
  pgvector/pgvector:pg17 \
  sh -c 'pg_dump "$DATABASE_URL"' \
  > "/opt/multica/backups/multica-test-$(date +%F-%H%M%S).sql"
```

恢复前应先停止 Backend，避免恢复过程中继续写入。

---

## 10. 配置维护规则

1. `.env.test` 是测试 VPS 环境变量的来源；
2. 测试 VPS 上 `.env` 由 `.env.test` 复制生成；
3. 修改测试环境配置时，应同步修改仓库 `.env.test`；
4. `.env.test` 包含测试环境密钥，只能用于测试环境，不要复用到生产；
5. 如果密钥曾经外泄，直接轮换 `JWT_SECRET`、数据库密码、邮件服务 key 和其他 token；
6. 如果新增服务端必须读取的环境变量，确认 `docker-compose.selfhost.yml` 的 Backend 仍然通过 `env_file` 注入 `.env`。

---

## 11. 常见问题

### 11.1 改了 `.env.test` 但服务没变化

先确认测试 VPS 上已经重新复制：

```bash
cp .env.test .env
make selfhost-build
```

再检查 Compose 展开的最终配置：

```bash
docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml config
```

### 11.2 Web 可以打开，但 `/api/config` 失败

优先看 Backend 日志：

```bash
docker compose -f docker-compose.selfhost.yml logs -f backend
```

常见原因：

1. 远程 PostgreSQL 无法从测试 VPS 访问；
2. `DATABASE_URL` 配置错误；
3. 数据库迁移失败；
4. `JWT_SECRET`、CAS、邮件等环境变量不符合生产安全检查；
5. Caddy 没有正确代理到 `localhost:3000`。

### 11.3 SSO 回调失败

检查 `.env.test` 中这些值是否和测试域名一致：

```env
FRONTEND_ORIGIN=https://multica.micoplatform.com
MULTICA_APP_URL=https://multica.micoplatform.com
MULTICA_SERVER_URL=wss://multica.micoplatform.com/ws
CAS_SERVICE_URL=https://multica.micoplatform.com/auth/cas/callback
```

同时确认 CAS 平台已登记同一个 callback 地址。

### 11.4 WebSocket 连接失败

检查 Caddy 是否包含 `/ws` 特殊代理：

```caddy
@multica_ws path /ws /ws/*
handle @multica_ws {
    reverse_proxy localhost:8080 {
        flush_interval -1
    }
}
```

然后重载 Caddy。
