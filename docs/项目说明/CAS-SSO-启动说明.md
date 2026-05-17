# Multica CAS SSO 启动说明

## 1. 关键概念

CAS 配置里最容易混淆的是 `CAS_LOGIN_URL` 和 `CAS_SERVICE_URL`。

| 变量 | 含义 | 当前建议 |
|---|---|---|
| `CAS_LOGIN_URL` | CAS 服务端的登录地址，Multica 会把浏览器重定向到这里 | 通常是 CAS base URL 后追加 `/login` |
| `CAS_VALIDATE_URL` | CAS 服务端的 ticket 校验地址，Multica 后端会请求这里 | 你已提供 |
| `CAS_SERVICE_URL` | Multica 后端的 CAS callback 地址，CAS 登录成功后会回跳这里 | 必须是外网可访问的 Multica 地址 |

也就是说，`CAS_SERVICE_URL` 不是 CAS 服务端地址，而是 Multica 自己的回调地址。

当前后端 callback 路由是：

```txt
/auth/cas/callback
```

如果本地调试需要经过公网回调，可以先用 ngrok、Cloudflare Tunnel、内网网关或测试环境域名暴露本地后端。

---

## 2. 你当前提供的 CAS 信息

CAS 服务端 base URL 看起来是：

```env
CAS_BASE_URL=https://micous-idp.cig.tencentcs.com/sso/tn-456d1d3feb5f4e09ad28ab35ee4d2e66/ai-4919cf3d4883490b956b90376cfb86e7/cas
```

据此推测：

```env
CAS_LOGIN_URL=https://micous-idp.cig.tencentcs.com/sso/tn-456d1d3feb5f4e09ad28ab35ee4d2e66/ai-4919cf3d4883490b956b90376cfb86e7/cas/login
CAS_VALIDATE_URL=https://micous-idp.cig.tencentcs.com/sso/tn-456d1d3feb5f4e09ad28ab35ee4d2e66/ai-4919cf3d4883490b956b90376cfb86e7/cas/serviceValidate
```

`CAS_LOGIN_URL` 不能在 `CAS_ENABLED=true` 时留空。后端启动时会校验它；留空会导致 CAS 登录不可用。

如果暂时还不知道登录地址，可以先保持：

```env
CAS_ENABLED=false
```

等确认登录地址后再启用 CAS。

---

## 3. 本地开发环境变量示例

编辑项目根目录 `.env`：

```env
CAS_ENABLED=true
CAS_DISPLAY_NAME=公司 SSO

CAS_LOGIN_URL=https://micous-idp.cig.tencentcs.com/sso/tn-456d1d3feb5f4e09ad28ab35ee4d2e66/ai-4919cf3d4883490b956b90376cfb86e7/cas/login
CAS_VALIDATE_URL=https://micous-idp.cig.tencentcs.com/sso/tn-456d1d3feb5f4e09ad28ab35ee4d2e66/ai-4919cf3d4883490b956b90376cfb86e7/cas/serviceValidate

# 本地调试时不能直接用 localhost，CAS 服务端必须能访问这个地址。
# 示例：把本地 8080 通过公网隧道暴露后，填公网 callback。
CAS_SERVICE_URL=https://<your-public-api-domain>/auth/cas/callback

# CAS 返回字段：
# - <cas:user> 作为邮箱
# - attributes.displayName 作为姓名
# - attributes.avatarOrigin 作为头像
CAS_ATTRIBUTE_EMAIL=user
CAS_ATTRIBUTE_NAME=displayName
CAS_ATTRIBUTE_AVATAR=avatarOrigin
CAS_EMAIL_DOMAIN=

# 如果公司要求只允许 SSO 登录：
EMAIL_LOGIN_ENABLED=false
GOOGLE_LOGIN_ENABLED=false
```

如果 CAS 的 `<cas:user>` 不是邮箱，而是工号或 username，则不能用 `CAS_ATTRIBUTE_EMAIL=user`。这种情况下需要二选一：

1. 让 CAS attributes 返回邮箱字段，例如 `email`；
2. 设置 `CAS_EMAIL_DOMAIN=company.com`，让 Multica 用 `user + @company.com` 拼出邮箱。

---

## 4. 启动本地开发服务

先确认 `.env` 里的 `DATABASE_URL` 指向可用数据库，然后执行：

```bash
make setup
make start
```

当前你的环境已经使用远程 PostgreSQL，可以直接：

```bash
make start
```

启动后访问：

```txt
http://localhost:3000/login
```

后端地址：

```txt
http://localhost:8080
```

---

## 5. 本地 CAS 回调调试方式

CAS 服务端需要回跳 Multica 后端的 `/auth/cas/callback`，所以本地调试必须让 CAS 服务端能访问你的后端。

一种常见方式是用公网隧道暴露本地 8080：

```bash
ngrok http 8080
```

假设得到：

```txt
https://abc123.ngrok-free.app
```

则配置：

```env
CAS_SERVICE_URL=https://abc123.ngrok-free.app/auth/cas/callback
FRONTEND_ORIGIN=http://localhost:3000
```

登录完成后，Multica 后端会设置 cookie，然后重定向回前端。

---

## 6. 验证步骤

### 6.1 配置是否生效

访问：

```txt
http://localhost:8080/api/config
```

应看到类似：

```json
{
  "auth": {
    "email_login_enabled": false,
    "google_login_enabled": false,
    "cas": {
      "enabled": true,
      "display_name": "公司 SSO",
      "login_url": "http://localhost:8080/auth/cas/start"
    }
  }
}
```

### 6.2 登录页

访问：

```txt
http://localhost:3000/login
```

预期：

1. 展示“使用公司 SSO 登录”；
2. 如果 `EMAIL_LOGIN_ENABLED=false`，不展示邮箱验证码入口；
3. 如果 `GOOGLE_LOGIN_ENABLED=false`，不展示 Google 登录入口。

### 6.3 CAS 登录流程

点击“使用公司 SSO 登录”后，流程应为：

```txt
Multica 登录页
  -> /auth/cas/start
  -> CAS_LOGIN_URL
  -> CAS_SERVICE_URL (/auth/cas/callback)
  -> CAS_VALIDATE_URL 校验 ticket
  -> Multica 签发 JWT cookie
  -> 回到前端登录页或目标页面
```

---

## 7. CLI 登录验证

CAS Web 登录通过后，再验证 CLI：

```bash
multica setup self-host \
  --server-url http://localhost:8080 \
  --app-url http://localhost:3000

multica login
multica auth status
```

预期：

1. CLI 打开浏览器；
2. 浏览器通过公司 SSO 登录；
3. 登录完成后回到 CLI 授权确认页；
4. CLI 拿到 token；
5. `multica auth status` 显示已登录。

---

## 8. 常见问题

### `CAS_LOGIN_URL` 可以留空吗？

只有 `CAS_ENABLED=false` 时可以留空。

只要 `CAS_ENABLED=true`，`CAS_LOGIN_URL`、`CAS_VALIDATE_URL`、`CAS_SERVICE_URL` 都必须填写。

### `CAS_SERVICE_URL` 应该填 CAS 服务端地址吗？

不应该。

`CAS_SERVICE_URL` 应该填 Multica 后端 callback 地址：

```txt
https://<your-public-api-domain>/auth/cas/callback
```

### `CAS_ATTRIBUTE_EMAIL=user` 是什么意思？

表示使用 CAS XML 里的 `<cas:user>` 作为邮箱。

如果 `<cas:user>` 返回的不是邮箱，就不要这么配。

### 为什么本地不能直接填 `http://localhost:8080/auth/cas/callback`？

因为 CAS 服务端在云端，它访问不到你电脑上的 localhost。

本地调试需要公网隧道，或直接部署一套测试环境。
