<div align="center">

# Fool Chat Admin

**Fool Chat 即时通讯系统 · 企业级后台管理平台**

基于 Go + MySQL 构建的轻量、高性能、AI 原生的后台管理系统

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://golang.org/)
[![MySQL](https://img.shields.io/badge/MySQL-8.0+-4479A1?logo=mysql&logoColor=white)](https://www.mysql.com/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](#license)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20Windows-lightgrey)]()
[![Status](https://img.shields.io/badge/Status-Production-brightgreen)]()

[功能特性](#-功能特性) · [快速开始](#-快速开始) · [架构设计](#-架构设计) · [API 文档](#-api-文档) · [部署指南](#-部署指南)

---

## 📖 项目简介

**Fool Chat Admin** 是 Fool Chat 即时通讯系统的官方后台管理平台，面向运营、审核、技术支持等管理员角色，提供从用户治理、内容审核、消息触达到数据洞察的全链路能力。

平台采用 Go 原生 HTTP 服务 + 单页前端的极简架构，部署一个二进制即可运行；同时集成 **AI 助手**，管理员可通过自然语言完成查询、修改、通知等高频操作，显著降低运维门槛。

### 设计目标

| 目标 | 说明 |
| :--- | :--- |
| **轻量部署** | 单二进制 + `config.yaml`，无需 Docker、无中间件依赖 |
| **零前端构建** | 模板 + 原生 JS，免 Node 工具链，热修不阻塞 |
| **操作可审计** | 所有关键动作落地 `admin_operation_log`，可追溯、可导出 |
| **AI 优先** | 自然语言驱动后台操作，兼容 OpenAI 协议的任意模型服务 |
| **安全可控** | 双重权限模型 + 高危操作二级密码 + 角色越权防护 |

---

## ✨ 功能特性

### 🔐 权限与认证
- 双角色体系：**管理员**（`role=1`）/ **超级管理员**（`role=2`）
- 客户端一致的 **SHA-256** 密码哈希校验
- 角色越权防护：禁止修改自身角色、禁止删除同级/上级账号
- **二级验证密码**保护删除等高危操作

### 📊 数据概览
- 用户、动态、申请、通知等核心指标实时统计
- 近 14 天动态趋势折线图、好友申请状态分布
- 今日新增、待审、异常操作多维监控
- 操作日志全文检索、模块/日期筛选、一键导出 Excel

### 👥 用户管理
- 分页、搜索（UID / 用户名 / 邮箱 / 昵称）、批量操作
- 创建、编辑、重置密码、角色任命
- 删除用户级联清理动态、好友、通知等关联数据

### 📝 动态审核
- 三态流转：正常 / 审核中 / 违规隐藏
- 内容编辑、点赞数调整、状态批量变更
- 支持发布、隐藏、恢复、删除全生命周期管理

### 🤝 关系与触达
- 好友关系查询、解绑（解绑后申请自动恢复待处理）
- `StarNotice` 公告与 `admin_notice` 后台通知分离管理
- 通知支持 **广播 / 定向**、四级别（`INFO` / `SUCCESS` / `WARNING` / `ERROR`）

### 🤖 AI 助手（核心亮点）
- 兼容 **OpenAI 协议** 的任意大模型（默认 DeepSeek，可切商汤、Kimi、智谱等）
- 自然语言驱动 9 类后台动作：查询用户/动态/日志、改状态、删除、发通知、切主题、换壁纸
- 高危动作 `requires_confirm` 二次确认机制，删除类需二级密码
- 多轮会话持久化（`ai_chat_message`），按 `session_id` 隔离
- 未配 API Key 时降级为关键词规则兜底，保证可用性
- 所有 AI 执行动作落 `admin_operation_log`，模块标记为 `ai`

### 🎨 界面体验
- 浅色 / 暗色双主题，壁纸自定义与预览
- Toast 状态反馈、危险操作二次确认弹窗
- 表格分页、长文本换行、刷新保留导航上下文

---

## 🚀 快速开始

### 前置要求

| 组件 | 版本 |
| :--- | :--- |
| Go | 1.25+ |
| MySQL | 5.7+ / 8.0+ |
| 浏览器 | Chrome / Edge / Firefox 现代版 |

### 本地运行

```bash
# 1. 克隆仓库
git clone <repo-url> fool_chat_admin_go
cd fool_chat_admin_go

# 2. 配置数据库与 AI 凭证
cp config.yaml.example config.yaml   # 编辑 database / ai 字段

# 3. 启动服务
go run .
```

默认监听 `http://0.0.0.0:9100`，浏览器访问即可登录后台。

### 编译产物

```bash
# Windows 本地
go build -o fool_chat_admin_go.exe .

# Linux 服务器（在 Windows PowerShell 中交叉编译）
$env:GOOS="linux"; $env:GOARCH="amd64"
go build -o fool_chat_admin_go .
```

---

## 🏗 架构设计

### 技术栈

| 层级 | 技术选型 |
| :--- | :--- |
| **后端** | Go 1.25 · `net/http` 原生路由 · `database/sql` |
| **数据库** | MySQL · `go-sql-driver/mysql` |
| **前端** | HTML5 · 原生 JavaScript · CSS3 |
| **导出** | SheetJS（前端 Excel 生成） |
| **AI** | OpenAI 兼容协议（DeepSeek / 商汤日日新等） |
| **运维** | systemd · SCP 部署 |

### 项目结构

```text
fool_chat_admin_go/
├── main.go                  # 入口、路由分发、Schema 自检
├── config.go                # YAML + 环境变量配置加载
├── auth.go                  # 登录态、Session、管理员权限校验
├── db.go                    # 数据库连接与查询辅助
├── log.go                   # 操作日志统一写入
├── util.go                  # 通用工具函数
├── handler_user.go          # 用户管理 / 密码 / 角色任命
├── handler_dynamic.go       # 动态 CRUD 与审核状态机
├── handler_notice.go        # 好友关系 / 公告 / 后台通知
├── handler_stats.go         # 概览统计 / 趋势分析 / 日志查询
├── handler_ai.go            # AI 对话 / 动作调度 / 会话管理
├── config.yaml              # 运行时配置
├── fool-chat-admin.service  # systemd 服务定义
├── templates/index.html     # 后台 SPA 模板
└── static/                  # 前端资源（JS/CSS/图标）
```

### 核心数据表

| 表 | 用途 | 自动创建 |
| :--- | :--- | :---: |
| `admin_notice` | 后台通知（广播/定向） | ✅ |
| `admin_operation_log` | 全量操作审计日志 | ✅ |
| `ai_chat_message` | AI 助手对话历史 | ✅ |
| `user` / `dynamic` / `friend` / `friend_apply` / `StarNotice` | 业务表（需预先存在） | ❌ |

> 服务启动时仅自动创建后台管理相关表，业务表结构变更建议通过独立 SQL 迁移执行，避免启动时静默改库。

---

## ⚙ 配置说明

### `config.yaml`

```yaml
app:
  addr: 0.0.0.0:9100              # 服务监听地址

database:
  host: 127.0.0.1
  port: 3306
  name: mhkh
  user: root
  password: your_password

security:
  session_key: fool-chat-admin
  delete_password: your_2fa_pwd   # 高危操作二级密码

ai:
  base_url: https://api.deepseek.com
  api_key: sk-xxxxxxxxxxxx
  model: deepseek-chat
```

### 环境变量覆盖

生产环境推荐使用环境变量注入敏感配置：

| 环境变量 | 对应字段 | 示例 |
| :--- | :--- | :--- |
| `APP_ADDR` | `app.addr` | `0.0.0.0:9100` |
| `DB_HOST` | `database.host` | `10.0.0.1` |
| `DB_PORT` | `database.port` | `3306` |
| `DB_NAME` | `database.name` | `mhkh` |
| `DB_USER` | `database.user` | `root` |
| `DB_PASSWORD` | `database.password` | `***` |

---

## 📡 API 文档

> Base URL: `http(s)://<host>:<port>/api`
> 除 `/api/login`、`/api/logout` 外，所有接口均需登录态。

### 认证

| 方法 | 路径 | 说明 |
| :--- | :--- | :--- |
| `POST` | `/api/login` | 管理员登录 |
| `POST` | `/api/logout` | 退出登录 |

### 概览与日志

| 方法 | 路径 | 说明 |
| :--- | :--- | :--- |
| `GET` | `/api/summary` | 概览指标、最近登录、异常列表 |
| `GET` | `/api/analytics` | 动态趋势、申请统计、通知送达 |
| `GET` | `/api/logs` | 操作日志（搜索/筛选/日期/导出） |
| `GET` | `/api/log-operators` | 日志中出现过的操作者下拉数据 |
| `POST` | `/api/get-bg-url` | 壁纸代理（图片转 data URL） |

### 用户管理

| 方法 | 路径 | 说明 |
| :--- | :--- | :--- |
| `GET` | `/api/users` | 用户列表（`q`/`page`/`limit`） |
| `POST` | `/api/users` | 创建用户 |
| `PATCH` | `/api/users/:uid` | 编辑资料 |
| `PATCH` | `/api/users/:uid/password` | 重置密码 |
| `PATCH` | `/api/users/:uid/role` | 任命/取消管理员 |
| `DELETE` | `/api/users/:uid` | 删除用户 ⚠ 需 `X-Delete-Password` |

### 动态管理

| 方法 | 路径 | 说明 |
| :--- | :--- | :--- |
| `GET` | `/api/dynamics` | 动态列表（`q`/`status`/`page`/`limit`） |
| `POST` | `/api/dynamics` | 发布动态 |
| `PATCH` | `/api/dynamics/:id` | 编辑内容与点赞数 |
| `PATCH` | `/api/dynamics/:id/status` | 切换状态（0/1/2） |
| `DELETE` | `/api/dynamics/:id` | 删除动态 |

### 好友与通知

| 方法 | 路径 | 说明 |
| :--- | :--- | :--- |
| `GET` | `/api/friend-applies` | 好友申请列表 |
| `PATCH` | `/api/friend-applies/:id` | 同意/拒绝/重置申请 |
| `GET` | `/api/friends` | 好友关系列表 |
| `DELETE` | `/api/friends/:self_id/:friend_id` | 解除双向好友 |
| `GET/POST/PATCH/DELETE` | `/api/star-notices` | 公告 CRUD |
| `GET/POST/PATCH/DELETE` | `/api/admin-notices[/:id]` | 后台通知 CRUD |
| `PATCH` | `/api/admin-notices/:id/delivered` | 标记已送达 |

### AI 助手

| 方法 | 路径 | 说明 |
| :--- | :--- | :--- |
| `POST` | `/api/ai/chat` | 发送消息（含动作 / 确认 / 二级密码） |
| `GET` | `/api/ai/sessions` | 当前管理员会话列表 |
| `GET` | `/api/ai/sessions/:id` | 会话完整消息（含 action / result） |
| `DELETE` | `/api/ai/sessions/:id` | 删除会话 |

<details>
<summary><strong>📥 /api/ai/chat 请求体</strong></summary>

```json
{
  "message": "查询 UID 1001 的用户",
  "session_id": 0,
  "confirm": false,
  "delete_password": "",
  "pending_action": null
}
```

- `session_id=0` 时由服务端创建新会话并回传
- 上一轮响应 `requires_confirm=true` 时，再次调用需带 `confirm=true` 与原 `pending_action`
- `delete_user` 类动作确认时必须携带 `delete_password`
</details>

<details>
<summary><strong>📤 /api/ai/chat 响应体</strong></summary>

```json
{
  "reply": "已切换为暗黑模式。",
  "action": { "name": "set_theme", "args": { "mode": "dark" } },
  "requires_confirm": false,
  "result": null,
  "session_id": 1718700000000
}
```
</details>

---

## 🚢 部署指南

### 标准部署目录

```text
/opt/fool_chat_admin_go/
├── fool_chat_admin_go        # 二进制
├── config.yaml
├── templates/
└── static/
```

### 上传产物

```bash
scp ./fool_chat_admin_go  root@<server>:/opt/fool_chat_admin_go/
scp ./config.yaml         root@<server>:/opt/fool_chat_admin_go/
scp -r ./templates        root@<server>:/opt/fool_chat_admin_go/
scp -r ./static           root@<server>:/opt/fool_chat_admin_go/
```

### systemd 托管

将 `fool-chat-admin.service` 上传至 `/etc/systemd/system/`，然后：

```bash
systemctl daemon-reload
systemctl enable --now fool-chat-admin
systemctl status fool-chat-admin

# 后续更新
systemctl restart fool-chat-admin

# 实时日志
journalctl -u fool-chat-admin -f
```

### 一键更新脚本

```bash
GOOS=linux GOARCH=amd64 go build -o fool_chat_admin_go .
scp ./fool_chat_admin_go root@<server>:/opt/fool_chat_admin_go/fool_chat_admin_go
ssh root@<server> "systemctl restart fool-chat-admin"
```

---

## 🛡 安全说明

- 密码全程 **SHA-256** 哈希存储与传输，前后端算法一致
- 删除用户、AI 删除动作要求 `security.delete_password` 二级密码
- Session 基于内存映射，进程重启失效（适合单实例部署）
- 所有写操作落 `admin_operation_log`，含 IP、UA、操作者、详细 JSON
- 角色权限三道防线：路由校验 → 角色校验 → 自我保护校验

> 🔒 生产部署建议：使用环境变量注入密钥、配合 Nginx HTTPS 反向代理、限制 MySQL 远程访问白名单。

---

## 🗺 路线图

- [ ] 多实例 Session 共享（Redis 后端）
- [ ] 操作日志的细粒度权限隔离
- [ ] AI 动作扩展：好友关系治理、批量通知模板
- [ ] WebSocket 实时推送审核任务
- [ ] 国际化（i18n）支持



</div>
