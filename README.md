# Fool Chat Admin

> Web 管理后台 · Go + MySQL + 原生前端 · 内置 AI 管理助手与数据备份能力

Fool Chat Admin 是 [Fool Chat](https://github.com/zzt2713/Fool_Chat_Chat_Admin) 即时通讯系统的 Web 管理后台。面向运营、审核与系统管理员，提供用户、动态、好友、公告、通知、操作日志、数据概览、数据备份和 AI 助手等一站式管理能力。

项目使用 Go 标准库 `net/http` 直接对外提供服务，前端无构建步骤，单二进制 + 静态目录即可部署。

![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)
![MySQL](https://img.shields.io/badge/MySQL-5.7%2F8.0-4479A1?logo=mysql&logoColor=white)
![License](https://img.shields.io/badge/license-MIT-blue)
![Status](https://img.shields.io/badge/status-active-success)

## 导航

- **账号 & 权限**：登录、Session、二级密码、基于 `role` 的层级权限校验
- **用户管理**：CRUD、批量删除、任命/撤销管理员、重置密码、级联清理
- **内容审核**：动态列表筛选、状态切换、批量隐藏、按关键词批量处理
- **关系治理**：好友申请处理、好友关系解除、申请状态可审计
- **公告 / 通知**：StarNotice 公告、admin_notice 投递（广播 / 定向 + 等级）
- **操作日志**：所有写操作落 `admin_operation_log`，支持搜索、筛选、导出
- **数据看板**：14 项核心指标 + 14 天趋势图 + 申请通过率 + 最近异常
- **数据维护 / 备份**：用户 / 动态 / 日志 CSV 导出、一键 CSV ZIP、一键多 Sheet Excel、数据库连接信息可视化
- **找回密码**：基于 VarifyServer 的邮箱验证码三步重置
- **AI 助手**：自然语言驱动、动作白名单、高危确认、多会话历史
- **前端体验**：随机壁纸 / 本地图片 / URL 图片 / 清除背景 / 暗黑模式 / 沉浸预览

## 技术栈

| 层级 | 技术 |
| --- | --- |
| 后端 | Go 1.21+，标准库 `net/http` |
| 数据访问 | `database/sql` + `go-sql-driver/mysql`（裸 SQL，无 ORM） |
| 数据库 | MySQL 5.7+ / 8.0 |
| 前端 | 原生 HTML / CSS / JavaScript，无构建 |
| 表格导出 | [SheetJS](https://github.com/SheetJS/sheetjs) 浏览器侧生成 Excel |
| AI | OpenAI （使用的 DeepSeek-v4-flash） |
| 部署 | ystemd |

## 快速开始

### 1. 准备环境

```bash
git clone https://github.com/zzt2713/Fool_Chat_Chat_Admin.git
cd Fool_Chat_Chat_Admin
go mod download
cp config.example.yaml config.yaml
# 按需修改 config.yaml 中的数据库 / 二级密码 / AI Key
```

### 2. 本地运行

```bash
go run .
```

默认监听 `0.0.0.0:9100`，浏览器打开 `http://127.0.0.1:9100`。

### 3. 编译

```bash
# Windows
go build -o fool_chat_admin_go.exe .

# Linux amd64 交叉编译（Windows PowerShell）
$env:GOOS="linux"; $env:GOARCH="amd64"
go build -o fool_chat_admin_go .

# Linux amd64 交叉编译（macOS / Linux）
GOOS=linux GOARCH=amd64 go build -o fool_chat_admin_go .
```

## 配置

`config.yaml` 字段：

```yaml
app:
  addr: 0.0.0.0:9100

database:
  host: 127.0.0.1
  port: 3306
  name: mhkh
  user: root
  password: your_database_password

security:
  session_key: fool-chat-admin
  delete_password: your_second_confirm_password

redis:
  host: 127.0.0.1
  port: "6379"
  password: ""

verify_server:
  addr: 127.0.0.1:50051

ai:
  base_url: https://api.deepseek.com
  api_key: your_ai_api_key
  model: deepseek-chat
```

## 项目结构

```
fool_chat_admin_go/
├── main.go                       # 入口 + 路由分发 + Schema 初始化
├── auth.go                       # 登录 / Session / 权限
├── db.go / config.go / util.go   # 数据库 / 配置 / 通用工具
├── log.go                        # 操作日志统一落库
├── handler_user.go               # 用户管理
├── handler_dynamic.go            # 动态管理
├── handler_notice.go             # 好友 / 公告 / 通知
├── handler_stats.go              # 概览、趋势、日志
├── handler_monitor.go            # 服务状态监控
├── handler_maintenance.go        # 数据维护与备份导出
├── handler_password_reset.go     # 找回密码三步流程
├── handler_ai.go                 # AI 助手对话与动作调度
├── verify_client.go              # gRPC 调 VarifyServer
├── templates/index.html          # 单页前端模板
└── static/
    ├── app.js / app.css          # 前端逻辑与样式
    ├── icon.png			     # 网页icon
    └── xlsx.full.min.js          # Excel 导出依赖
```

## 数据维护 / 备份

后台「数据维护」页提供：

- **数据量概览**：用户、动态、日志、好友、申请、通知等核心表行数
- **数据库连接信息**：driver / host / port / database / user
- **CSV 导出**：用户、动态、日志可单独导出 CSV（UTF-8 BOM，Excel 直接打开不乱码）
- **一键 CSV 压缩包**：服务端打包三类 CSV 为 ZIP 下载
- **一键 Excel**：浏览器侧通过 SheetJS 拉取全量数据并生成多 Sheet `.xlsx`

后端接口：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/maintenance/summary` | 数据量 + 数据库连接信息 |
| `GET` | `/api/maintenance/export?type=users\|dynamics\|logs\|all` | CSV 或 ZIP 下载 |

## AI 管理助手

AI 助手不直接写 SQL，而是把自然语言转换为受控的结构化动作，由 Go 后端按白名单执行。可在未配置 `ai.api_key` 时使用关键词兜底逻辑保证离线可用。

### 请求示例

```http
POST /api/ai/chat
Content-Type: application/json
Cookie: fool_chat_admin_session=...
```

```json
{
  "message": "把待审核动态按 UID 分组",
  "session_id": 0,
  "confirm": false
}
```

### 安全设计

- 模型只能输出 JSON，后端走动作白名单
- 高危动作统一走前端二次确认弹窗
- 删除用户额外要求二级密码
- AI 触发的写操作全部落 `admin_operation_log`
- 多会话历史落 `ai_chat_message`，按 `session_id` 切换

## API 概览

> 除 `/api/login`、`/api/logout`、`/api/password-reset/*` 外均需登录态。

| 模块 | 主要接口 |
| --- | --- |
| 认证 | `POST /api/login`、`POST /api/logout` |
| 找回密码 | `POST /api/password-reset/{lookup,send,reset}` |
| 概览 | `GET /api/summary`、`GET /api/analytics`、`GET /api/service-status` |
| 维护 | `GET /api/maintenance/summary`、`GET /api/maintenance/export` |
| 日志 | `GET /api/logs`、`GET /api/log-operators` |
| 用户 | `GET/POST /api/users`、`PATCH/DELETE /api/users/{uid}` |
| 动态 | `GET/POST /api/dynamics`、`PATCH/DELETE /api/dynamics/{id}` |
| 好友 | `GET /api/friend-applies`、`PATCH /api/friend-applies/{id}`、`GET /api/friends`、`DELETE /api/friends/{a}/{b}` |
| 公告 | `GET/POST/PATCH/DELETE /api/star-notices` |
| 通知 | `GET/POST /api/admin-notices`、`PATCH/DELETE /api/admin-notices/{id}` |
| AI | `POST /api/ai/chat`、`GET/DELETE /api/ai/sessions/{id}` |

## Linux 部署

### systemd（推荐）

`/etc/systemd/system/fool-chat-admin.service`：

```ini
[Unit]
Description=Fool Chat Admin
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/fool_chat_admin_go
ExecStart=/opt/fool_chat_admin_go/fool_chat_admin_go
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now fool-chat-admin
sudo systemctl status fool-chat-admin
```

### 上传部署文件

```bash
scp ./fool_chat_admin_go      root@<server>:/opt/fool_chat_admin_go/
scp ./config.yaml             root@<server>:/opt/fool_chat_admin_go/
scp -r ./templates ./static   root@<server>:/opt/fool_chat_admin_go/
ssh root@<server> "systemctl restart fool-chat-admin"
```

> 改 Go 代码 → 必须重新编译并替换二进制；只改前端可只上传 `static/`、`templates/`。

## 安全策略

- 后台仅允许 `role >= 1` 登录
- 管理员不能操作自己 / 操作权限 ≥ 自己的账号
- 删除用户走二级密码校验（`security.delete_password`）
- 所有写操作记录 `module / action / target / operator / ip / detail_json`
- 推荐 Nginx 反代 + HTTPS，MySQL 仅放行内网

## Roadmap

- [ ] RBAC 菜单 / 按钮级权限
- [ ] Session 迁移 Redis 支持多实例
- [ ] 数据库迁移脚本替代启动自动建表
- [ ] AI 操作审批流（双人确认）
- [ ] 通知投递回执统计
- [ ] 集成测试

## License

MIT. 见 [LICENSE](./LICENSE)（如未提供，请根据需要补充）。

## 致谢

- [go-sql-driver/mysql](https://github.com/go-sql-driver/mysql)
- [SheetJS](https://github.com/SheetJS/sheetjs)
- [DeepSeek](https://www.deepseek.com/) / OpenAI Compatible API
