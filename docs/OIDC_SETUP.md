# OAuth SSO 配置指南

## 支持的 Provider

| Provider | 环境变量 | 需要什么 |
|----------|----------|----------|
| **Google** | `OAUTH_GOOGLE_CLIENT_ID` / `OAUTH_GOOGLE_CLIENT_SECRET` | Google Cloud Console OAuth App |
| **GitHub** | `OAUTH_GITHUB_CLIENT_ID` / `OAUTH_GITHUB_CLIENT_SECRET` | GitHub OAuth App |
| **微信** | `OAUTH_WECHAT_APP_ID` / `OAUTH_WECHAT_APP_SECRET` | 微信开放平台应用 |

## 配置步骤

### 1. 创建 OAuth App

**Google:**
1. https://console.cloud.google.com → APIs & Services → Credentials
2. Create OAuth Client ID → Web application
3. Authorized redirect URIs: `http://localhost:8001/api/account/v1/oauth/google/callback`
4. 复制 Client ID 和 Client Secret

**GitHub:**
1. https://github.com/settings/developers → OAuth Apps → New
2. Authorization callback URL: `http://localhost:8001/api/account/v1/oauth/github/callback`
3. 复制 Client ID 和 Client Secret

### 2. 环境变量

```bash
# 公共配置
OAUTH_REDIRECT_BASE=http://localhost:8001/api/account/v1/oauth
OAUTH_SUCCESS_URL=http://localhost:5173/oauth-callback

# Google
OAUTH_GOOGLE_CLIENT_ID=xxx.apps.googleusercontent.com
OAUTH_GOOGLE_CLIENT_SECRET=xxx

# GitHub
OAUTH_GITHUB_CLIENT_ID=xxx
OAUTH_GITHUB_CLIENT_SECRET=xxx
```

### 3. 数据库迁移

```bash
mysql -h192.168.192.21 -P3307 -uroot -p aqicloud_account < sql/oidc_migration.sql
```

### 4. API 接口

```
GET /api/account/v1/oauth/providers          → 返回已配置的 provider 列表
GET /api/account/v1/oauth/:provider/login    → 302 到 provider 授权页
GET /api/account/v1/oauth/:provider/callback → 处理回调，签发 JWT
```

**providers 响应示例:**
```json
{"code":0, "data":[{"id":"google","name":"Google","url":"..."}, {"id":"github","name":"GitHub","url":"..."}]}
```

### 5. 前端集成

```javascript
// 方式一：直接跳转
window.location.href = '/api/account/v1/oauth/google/login?redirect=' + encodeURIComponent(currentUrl)

// 方式二：获取 URL 后自行处理
const res = await fetch('/api/account/v1/oauth/providers')
const providers = await res.json()
// 在登录页显示 Google/GitHub 按钮
```

回调时后端会重定向到 `OAUTH_SUCCESS_URL?token=dcloud-linkxxx`，前端提取 token 存 localStorage。
