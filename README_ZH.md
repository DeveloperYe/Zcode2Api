# ⚡ Zcode2API（中文版）

面向 Z.AI（ZCode Start Plan）的 OpenAI / Anthropic 兼容代理：JWT 账号池、额度耗尽自动切换、验证码处理、智能代理轮转。

[![版本](https://img.shields.io/badge/version-1.0.0-blue)](https://github.com/DeveloperYe/Zcode2Api/releases)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-AS_IS-green)](#license)

[![Telegram](https://img.shields.io/badge/Telegram-D3__vin-blue?logo=telegram)](https://t.me/D3_vin)
[![作者](https://img.shields.io/badge/Author-D3vin__dev-blue?logo=telegram)](https://t.me/D3vin_dev)
[![GitHub](https://img.shields.io/badge/GitHub-Repository-black?logo=github)](https://github.com/D3-vin/Zcode2Api)

[功能](#功能) • [快速开始](#快速开始) • [使用方法](#使用方法) • [配置](#配置) • [接口清单](#接口清单) • [联系](#联系)

[English](README.md) | [Русский](README_RU.md) | **中文**

> 本文件是上游英文 README 的中文翻译（简体中文）。上游代码以英文注释为准；本文件由中文分支维护，可能略滞后于上游最新提交。

---

## 功能

- 🚀 **单一可执行文件** —— 服务端、验证码处理、令牌池打包在一个二进制里
- 🔓 **双协议** —— Anthropic Messages（`/v1/messages`）+ OpenAI Chat（`/v1/chat/completions`）
- 🧩 **账号池** —— JWT 轮转，额度耗尽自动故障切换
- 🛡️ **智能切换** —— 处理 429、负载类错误（3012/3007/3009/3010/529）并轮转代理
- 📊 **实时面板** —— 账号额度、代理配置、实时监控
- 🌐 **代理支持** —— 粘性 / 轮转两种模式，失败自动暂停，IP 探测
- 🔐 **验证码处理** —— 内嵌 Node.js 模块解阿里云验证码
- 🎯 **动态模型** —— 模型目录随账号账单同步（当前为 **GLM-5.3** 与 **GLM-5.3-Flash**）
- 📦 **零外部依赖** —— Node.js 模块内嵌、SQLite 数据库，不需要额外服务

> 💡 **开箱即用：Cursor、OpenCode、Claude Code 以及任何 OpenAI / Anthropic 兼容客户端都可直接接入。**

---

## 快速开始

### 1. 下载

到 Releases 页面下载对应平台的构建产物：

- [Windows (64 位)](https://github.com/D3-vin/Zcode2Api/releases)
- [Linux (64 位)](https://github.com/D3-vin/Zcode2Api/releases)
- [macOS Intel](https://github.com/D3-vin/Zcode2Api/releases)
- [macOS Apple Silicon](https://github.com/D3-vin/Zcode2Api/releases)

或者从源码编译：

```bash
git clone https://github.com/D3-vin/Zcode2Api.git
cd Zcode2Api
go build -trimpath -ldflags="-s -w" -o zcode2api ./cmd/zcode2api
```

**环境要求：**

- **PATH 中可用的 Node.js**（验证码处理需要）
- Go 1.25+（自行编译时需要）

### 2. 配置

```bash
cp .env.example .env
# 编辑 .env —— 把 AUTH_TOKEN 改成你自己的口令
```

### 3. 启动

```bash
./zcode2api
```

面板地址：<http://localhost:3101>

### 4. 添加账号

**方式一：面板 OAuth 登录（推荐）**

1. 打开 <http://localhost:3101>，用你的 `AUTH_TOKEN` 登录
2. 点 **Login Z.AI** → 在浏览器里完成 OAuth 授权
3. JWT 自动进入账号池并刷新额度

**方式二：批量粘贴**

1. 面板 → **Add（批量）** → 粘贴 JWT 令牌（一行一个）

**方式三：文件导入**

1. 新建 `tokens.txt`，一行一个 JWT
2. 在 `.env` 里设置 `ZCODE_KEYS=tokens.txt`
3. 重启服务

### 5. 测试

**OpenAI 格式：**

```bash
curl -N http://localhost:3101/v1/chat/completions \
  -H "Authorization: Bearer d3vin" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "GLM-5.3",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": true
  }'
```

**Anthropic 格式：**

```bash
curl -N http://localhost:3101/v1/messages \
  -H "x-api-key: d3vin" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "GLM-5.3",
    "messages": [{"role": "user", "content": "Hello!"}],
    "max_tokens": 1024,
    "stream": true
  }'
```

> ⚠️ 上面的 `d3vin` 是默认口令，**务必改成你自己的**（见[配置](#配置)）。

---

## 使用方法

### IDE 接入

**OpenAI 兼容客户端（Cursor、OpenCode 等）：**

```
Base URL: http://localhost:3101/v1
API Key: d3vin  # 你的 AUTH_TOKEN
Model: GLM-5.3
```

**Anthropic 兼容客户端（Claude Code）：**

```
Base URL: http://localhost:3101
API Key: d3vin  # 你的 AUTH_TOKEN
Model: GLM-5.3
```

### 面板功能

**账号页（Accounts）：**

- 通过 OAuth、批量粘贴 JWT 或文件导入添加账号
- 查看额度、状态（在用 / 待用 / 冷却中 / 已耗尽 / 已吊销 / 已停用）
- 手动刷新额度，或按间隔自动刷新
- 领取可用套餐（带预览）
- 连设备身份一起导出 / 导入账号池（JSON）

**代理页（Proxies）：**

- 添加代理线路（粘性 / 轮转模式）
- 把代理绑定到指定账号（出口固定）
- 连续 2 次探测失败自动暂停
- IP 探测，可配置间隔

**设置页（Settings）：**

- 在线修改 `AUTH_TOKEN` 与 `GATEWAY_KEY`（写入数据库持久化）
- 配置账单并发、切换上限、验证码超时

---

## 接口清单

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/messages` | Anthropic Messages 接口（流式 / 非流式） |
| POST | `/v1/chat/completions` | OpenAI Chat Completions（自动转换为 Anthropic） |
| POST | `/async/v1/messages` | 异步票据式 SSE（ticket → ready → chunks → done） |
| GET | `/v1/models` | 可用模型（随账号账单同步） |
| GET | `/v1/usage` | 用量统计 |
| GET | `/api/health` | 服务健康检查 |
| GET | `/` | 管理面板（登录门禁在 `/login.html`） |

**鉴权：**

- 请求头：`Authorization: Bearer <token>` 或 `x-api-key: <token>`
- 令牌：`AUTH_TOKEN`（面板与 `/api/*`）或 `GATEWAY_KEY`（设置后用于 `/v1/*` 与 `/async`）

---

## 配置

全部配置都在 `.env` 里（完整示例见 `.env.example`）：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `HOST` | `127.0.0.1` | 监听地址 |
| `PORT` | `3101` | 监听端口 |
| `AUTH_TOKEN` | `d3vin` | 面板与 API 的鉴权口令 |
| `GATEWAY_KEY` | _（空）_ | `/v1/*` 专用密钥（为空则沿用 `AUTH_TOKEN`） |
| `ZCODE_KEYS` | _（空）_ | 启动时注入账号池的 JWT 文件（一行一个） |
| `PROXY_FILE` | `proxy.txt` | 空数据库时一次性导入的代理配置 |
| `LOAD_SWAPS` | `4` | 遇到负载类错误时的出口轮转次数（3012/3007/3009/3010） |
| `ACCOUNT_MAX_TRY` | `8` | 单个客户端请求最多尝试的账号数（故障切换） |
| `BILLING_WORKERS` | `25` | 额度刷新并发数 |
| `CAPTCHA_SOLVE_TIMEOUT` | `40s` | 验证码处理超时 |
| `CAPTCHA_RETRIES` | `3` | 验证码重试次数 |
| `CAPTCHA_MODE` | _（空）_ | 设为 `off` 可关闭后台预生成 |

**动态设置：**

- `AUTH_TOKEN` 与 `GATEWAY_KEY` 可在面板 → 设置里修改（写入数据库，重启后依然生效）

---

## 错误处理

| 错误类型 | 处理方式 |
|----------|----------|
| 模型额度耗尽（429） | 把账号标记为死号直到额度周期重置，换下一个账号 |
| 负载类错误（3012/3007/3009/3010/529） | 暂停、轮转出口、重新生成验证码、重试 |
| 带 `Retry-After` 的 429 | 等待（上限 120 秒），同一出口最多重试 5 次 |
| 401 未授权 | 标记为已吊销并从轮转中移除 |
| `max_tokens` > 131072 | 截断到 131072（上游超过此值返回 400/1210） |

**账号状态：**

- **在用（Active）** —— 可接请求
- **待用（Standby）** —— 可用但当前未被选中
- **冷却中（Cooling）** —— 累计 3 次风控 → 冷却 30 分钟
- **已耗尽（Exhausted）** —— 额度用尽，等待重置
- **已吊销（Revoked）** —— 401，JWT 失效
- **已过期（Expired）** —— JWT 过期
- **已停用（Disabled）** —— 手动关闭（额度刷新不会覆盖）

---

## 代理

**支持的格式：**

```
http://proxy.example.com:8080
http://user:pass@proxy.example.com:8080
https://proxy.example.com:443
socks5://proxy.example.com:1080
socks5://user:pass@proxy.example.com:1080
```

**行为：**

- 对话请求与账单请求按账号使用同一出口（粘性）
- 验证码生成始终从服务器本机直连
- 代理池为空即直连

**代理配置项：**

- **粘性模式（sticky）** —— 给账号绑定固定代理（出口锁定）
- **轮转模式（rotate）** —— 每个请求换一个代理
- **自动暂停** —— 连续 2 次探测失败后停用该代理
- **IP 探测** —— 定期检查出口 IP，间隔可配

---

## 故障排查

| 现象 | 原因 | 处理 |
|------|------|------|
| `captcha solver failed` | PATH 里没有 Node.js，或验证码超时 | 确认 `node` 命令可用，调大 `CAPTCHA_SOLVE_TIMEOUT` |
| `all accounts exhausted` | 没有尚有额度的可用账号 | 加账号，或等额度重置 |
| `429 rate limit` | 请求太频繁 | 服务端会自动退避重试；客户端也应做指数退避 |
| `proxy connection failed` | 代理不可用或网络问题 | 检查代理账号密码，在面板里停用坏线路 |
| 面板登录失败 | `AUTH_TOKEN` 不对 | 检查 `.env`，或在设置页更新口令 |

---

## 项目结构

```
Zcode2Api/
├── zcode2api                    # 二进制（Linux/macOS）
├── zcode2api.exe                # 二进制（Windows）
├── .env                         # 配置（从 .env.example 复制）
├── .env.example                 # 配置模板
├── proxy.txt                    # 代理种子文件（可选）
├── tokens.txt                   # JWT 种子文件（可选）
├── data/                        # 运行时数据目录
│   ├── zcode2api.db            # SQLite 数据库（账号、代理、设置）
│   └── captcha_node/           # 内嵌验证码模块（首次运行自动解包）
├── cmd/zcode2api/               # 主程序
├── internal/
│   ├── gateway/                 # 核心服务逻辑
│   ├── captcha/                 # 验证码处理集成
│   ├── store/                   # 数据库层
│   ├── upstream/                # Z.AI 客户端
│   ├── proxy/                   # 代理管理
│   └── config/                  # 配置加载
└── tests/                       # 测试
```

---

## 编译

**跨平台构建：**

```bash
# Windows AMD64
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o build/zcode2api-windows-amd64.exe ./cmd/zcode2api

# Linux AMD64
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o build/zcode2api-linux-amd64 ./cmd/zcode2api

# macOS Intel
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o build/zcode2api-macos-amd64 ./cmd/zcode2api

# macOS Apple Silicon
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o build/zcode2api-macos-arm64 ./cmd/zcode2api
```

**自定义版本号：**

```bash
go build -ldflags "-X main.AppVersion=1.0.1" -o zcode2api ./cmd/zcode2api
```

---

## 中文分支说明

本仓库是 <https://github.com/D3-vin/Zcode2Api> 的 fork，除上游代码外附加：

- `README_ZH.md`：本文件，中文说明
- `internal/gateway/webui/i18n-zh.js`：面板中文界面层（运行时不改动上游 HTML/JS，右下角可切换 中文 / EN）
- 修正设置页 `const sel = sel.innerHTML = …` 的自引用报错（上游会导致版本号与模型列表渲染失败）

同步上游：

```bash
git remote add upstream https://github.com/D3-vin/Zcode2Api.git
git fetch upstream && git merge upstream/main
```

---

## 联系

- **GitHub**：<https://github.com/D3-vin/Zcode2Api>
- **Telegram**：[@D3_vin](https://t.me/D3_vin)
- **作者**：[@D3vin_dev](https://t.me/D3vin_dev)

---

## 许可

按「现状」提供，仅用于学习与互操作目的 —— 详见 [LICENSE](LICENSE)。

## ⚠️ 免责声明

本软件**仅用于学习与互操作目的**，按「现状（AS IS）」提供，不附带任何形式的担保。作者不对任何误用及其后果负责 —— 责任完全由使用者承担。使用时请遵守 Z.AI 的服务条款。本项目与 Z.AI、阿里云无任何隶属关系。
