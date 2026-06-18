# Fool Chat Admin 管理后台

Fool Chat Admin 是 Fool Chat 即时通讯系统的 Web 管理后台，面向运营、审核、系统管理员和项目维护人员，提供用户管理、动态审核、好友关系管理、公告通知、操作日志、数据概览和 AI 管理助手等能力。

本项目采用 Go 原生 HTTP 服务 + MySQL + 原生 HTML/CSS/JavaScript 实现，部署形态简单，运行时只需要一个可执行文件、`config.yaml`、`templates/` 和 `static/` 目录，适合中小型后台管理系统、毕业设计演示环境以及单机生产部署场景。

---

## 1. 项目定位

### 1.1 所属系统

Fool Chat 是一个全栈即时通讯项目，整体由以下部分组成：

| 模块 | 技术栈 | 职责 |
| --- | --- | --- |
| 桌面客户端 | Qt / Qt Network | 用户登录注册、好友添加、即时聊天、动态发布、公告通知展示 |
| 网关服务 | C++ / Beast / gRPC | 对外提供登录注册 HTTP 接口，协调状态服务分配聊天节点 |
| 聊天服务 | C++ / Asio / gRPC | 维护 TCP 长连接、消息转发、动态与好友相关业务处理 |
| 状态服务 | C++ / gRPC / Redis | ChatServer 节点发现、负载记录、登录态存储 |
| 验证服务 | Node.js / gRPC | 邮件验证码发送与验证流程解耦 |
| 管理后台 | Go / MySQL / Web | 用户、动态、公告、通知、日志和 AI 助手管理 |

### 1.2 后台目标

Fool Chat Admin 的设计目标是：

- 为即时通讯系统提供可视化管理入口。
- 将高频运营操作沉淀为可审计、可回溯的后台动作。
- 对危险操作增加权限校验、二级密码和确认流程。
- 通过 AI 助手降低查询和日常运营操作成本。
- 保持部署简单，方便在 Windows 本地和 Linux 服务器上运行。

---

## 2. 技术架构

### 2.1 技术栈

| 层级 | 技术 |
| --- | --- |
| 后端语言 | Go |
| HTTP 服务 | Go 标准库 `net/http` |
| 数据访问 | `database/sql` + `go-sql-driver/mysql` |
| 数据库 | MySQL 5.7+ / MySQL 8.0 |
| 前端 | HTML5 / CSS3 / 原生 JavaScript |
| 导出 | SheetJS，本地生成 Excel 文件 |
| AI 接入 | OpenAI Compatible Chat Completions API |
| 部署方式 | 二进制部署 / systemd 托管 |

### 2.2 运行结构

```text
fool_chat_admin_go/
├── main.go                    # 服务入口、路由注册、模板渲染、Schema 初始化
├── config.go                  # config.yaml 与环境变量配置加载
├── db.go                      # MySQL 连接池初始化与通用查询能力
├── auth.go                    # 登录、退出、Session、角色权限校验
├── log.go                     # 后台操作日志统一写入
├── util.go                    # JSON 响应、参数解析、通用工具
├── handler_user.go            # 用户管理、密码重置、角色任命、删除保护
├── handler_dynamic.go         # 动态查询、发布、编辑、状态审核、删除
├── handler_notice.go          # 好友申请、好友关系、公告、后台通知
├── handler_stats.go           # 概览统计、趋势数据、日志查询
├── handler_ai.go              # AI 对话、动作调度、AI 会话历史
├── config.yaml                # 本地运行配置，生产环境可由环境变量覆盖
├── config.example.yaml        # 配置模板，不应写入真实密钥
├── fool-chat-admin.service    # systemd 服务配置示例
├── templates/
│   └── index.html             # 后台单页应用模板
└── static/
    ├── app.js                 # 前端业务逻辑
    ├── app.css                # 页面样式
    ├── icon.png               # 页面图标
    └── xlsx.full.min.js       # Excel 导出依赖
```

### 2.3 业务流程概览

```text
浏览器
  │
  │  HTTP / JSON
  ▼
Go Admin Server
  │
  ├── Session 校验
  ├── 角色权限校验
  ├── 参数校验与业务处理
  ├── 操作日志写入
  └── MySQL 数据访问
        │
        ▼
Fool Chat 业务数据库
```

AI 助手流程：

```text
管理员输入自然语言
  │
  ▼
/api/ai/chat
  │
  ├── 调用 OpenAI Compatible 模型生成结构化 action
  ├── 或在未配置 API Key 时使用关键词兜底规则
  ├── 判断是否为高危动作
  ├── 需要确认则返回 pending action
  ├── 客户端动作交给 app.js 执行
  └── 服务端动作由 Go 后端受控执行并写入日志
```

---

## 3. 功能清单

### 3.1 登录与权限

- 管理员账号登录。
- 基于 Session 的登录态维护。
- 支持角色字段 `role`：
  - `0`：普通用户，不能登录后台。
  - `1`：管理员，可使用常规后台功能。
  - `2`：超级管理员，拥有更高管理权限。
- 管理员不能任命自己。
- 管理员不能删除自己账号。
- 管理员不能删除或编辑权限高于或等于自己的账号。
- 删除用户等高危操作需要二级验证密码。

### 3.2 概览仪表盘

- 用户总数。
- 动态总数。
- 待处理好友申请数。
- 后台通知数量。
- 好友申请通过率。
- 通知送达率。
- 累计操作数。
- 今日操作数。
- 今日新增用户数。
- 今日动态数。
- 待审核动态数。
- 最近登录用户。
- 最近异常操作。
- 近 14 天动态发布趋势。
- 好友申请状态分布。

### 3.3 用户管理

- 用户列表查询。
- 按 UID、用户名、邮箱、昵称搜索。
- 创建用户。
- 编辑用户资料。
- 重置密码。
- 任命或取消管理员身份。
- 批量选择用户。
- 删除用户并级联清理相关数据。
- 用户数据导出 Excel。

### 3.4 动态管理

- 动态列表查询。
- 按 UID、用户名、动态内容搜索。
- 按状态筛选：正常、审核中、违规隐藏。
- 发布动态。
- 编辑动态内容和点赞数。
- 审核通过动态。
- 设置动态为审核中。
- 隐藏违规动态。
- 删除动态。
- 动态数据导出 Excel。

### 3.5 好友申请与好友关系

- 好友申请列表查看。
- 申请状态查看与处理。
- 好友关系列表查询。
- 按 UID、用户名、昵称搜索好友关系。
- 解除双向好友关系。

好友关系删除建议策略：

```sql
DELETE FROM friend
WHERE (self_id = A AND friend_id = B)
   OR (self_id = B AND friend_id = A);
```

如果希望删除好友后客户端不重新出现旧申请，建议不要把已经同意的 `friend_apply` 直接恢复为待处理，而是：

- 保留历史申请状态用于审计；或
- 新增独立状态表示“已解除关系”；或
- 删除旧申请记录，由用户重新发起新的好友申请。

### 3.6 公告管理

- `StarNotice` 公告列表查询。
- 新增公告。
- 编辑公告。
- 删除公告。
- 公告数据导出 Excel。

### 3.7 后台通知投递

- 创建后台通知。
- 支持广播通知和指定 UID 通知。
- 创建和编辑时支持从下拉框选择目标用户，展示格式为 `UID（用户名）`。
- 支持通知等级：`info`、`success`、`warning`、`error`。
- 编辑通知。
- 标记通知已处理或已送达。
- 删除通知。
- 通知数据导出 Excel。

### 3.8 操作日志

- 新增、编辑、删除、审核、任命、登录等关键操作写入 `admin_operation_log`。
- 支持按说明、操作人、模块搜索。
- 支持按模块、动作、日期范围筛选。
- 支持日志导出 Excel。
- 记录字段包括：模块、动作、目标对象、操作人、摘要、详细 JSON、IP、User-Agent、时间。

### 3.9 页面体验

- 明暗主题切换。
- 随机壁纸切换。
- 自定义背景上传。
- 当前壁纸预览。
- 下载当前壁纸。
- Toast 操作反馈。
- 删除等高危操作二次确认。
- 表格分页、搜索、筛选、导出。
- 页面刷新后保留当前导航视图。

---

## 4. AI 管理助手

### 4.1 功能定位

AI 管理助手不是让模型直接操作数据库，而是让模型把自然语言转换为受控的结构化动作。Go 后端只执行白名单中的动作，并在必要时要求管理员确认。

### 4.2 当前支持动作

#### 查询类动作

| 动作 | 说明 |
| --- | --- |
| `query_user` | 查询用户信息 |
| `query_dynamic` | 查询动态信息 |
| `query_logs` | 查询后台操作日志 |
| `query_summary` | 查询概览统计数据 |
| `query_friend_applies` | 查询好友申请 |
| `query_star_notices` | 查询公告 |
| `search_all` | 跨用户、动态、日志聚合搜索 |

#### 数据修改类动作

| 动作 | 说明 | 是否高危 |
| --- | --- | --- |
| `update_dynamic_status` | 修改动态状态 | 是 |
| `delete_dynamic` | 删除动态 | 是 |
| `delete_user` | 删除用户 | 是，且需要二级密码 |
| `send_notice` | 创建后台通知 | 是 |
| `batch_hide_dynamics_by_keyword` | 按关键词批量隐藏动态 | 是 |
| `update_user_role` | 修改用户角色 | 是 |

#### 页面操作类动作

| 动作 | 说明 |
| --- | --- |
| `switch_wallpaper` | 切换随机壁纸 |
| `download_wallpaper` | 下载当前壁纸 |
| `upload_wallpaper` | 打开自定义背景上传 |
| `toggle_bg_preview` | 开启或退出壁纸预览 |
| `set_theme` | 切换明暗主题 |
| `navigate` | 跳转到指定后台页面 |
| `refresh_view` | 刷新当前页面数据 |
| `logout` | 退出登录，属于高危动作，需要确认 |

### 4.3 安全设计

- AI 只能返回 JSON，不允许直接生成 SQL 交给后端执行。
- 后端使用动作白名单控制可执行范围。
- 高危动作统一走确认流程。
- 删除用户必须校验 `security.delete_password`。
- AI 执行动作会写入操作日志。
- AI 对话历史写入 `ai_chat_message`，按 `session_id` 区分会话。

### 4.4 请求示例

```http
POST /api/ai/chat
Content-Type: application/json
Cookie: fool_chat_admin_session=...
```

```json
{
  "message": "查询 uid 8 的用户",
  "session_id": 0,
  "confirm": false,
  "delete_password": "",
  "pending_action": null
}
```

响应示例：

```json
{
  "reply": "查询用户结果如下。",
  "action": {
    "name": "query_user",
    "args": {
      "uid": 8
    }
  },
  "requires_confirm": false,
  "result": [],
  "session_id": 1718700000000
}
```

---

## 5. 数据库说明

### 5.1 依赖业务表

后台依赖 Fool Chat 主业务数据库中的表，例如：

| 表名 | 说明 |
| --- | --- |
| `user` | 用户账号信息 |
| `dynamic` | 用户动态 |
| `friend` | 好友关系 |
| `friend_apply` | 好友申请 |
| `StarNotice` | 公告表 |

这些表属于即时通讯系统业务表，建议由主项目的 SQL 迁移脚本维护，不建议后台启动时自动修改业务表结构。

### 5.2 后台辅助表

后台会初始化或使用以下管理表：

#### `admin_notice`

用于后台通知投递。

```sql
CREATE TABLE IF NOT EXISTS admin_notice (
  id BIGINT NOT NULL AUTO_INCREMENT,
  target_uid INT NULL DEFAULT NULL,
  title VARCHAR(80) NOT NULL DEFAULT '',
  content TEXT NULL,
  level VARCHAR(20) NOT NULL DEFAULT 'info',
  delivered TINYINT NOT NULL DEFAULT 0,
  create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  INDEX idx_target_uid (target_uid),
  INDEX idx_delivered (delivered),
  INDEX idx_create_time (create_time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

#### `admin_operation_log`

用于后台操作审计。

```sql
CREATE TABLE IF NOT EXISTS admin_operation_log (
  id BIGINT NOT NULL AUTO_INCREMENT,
  module VARCHAR(40) NOT NULL DEFAULT '',
  action VARCHAR(40) NOT NULL DEFAULT '',
  target_type VARCHAR(40) NOT NULL DEFAULT '',
  target_id VARCHAR(80) NULL DEFAULT '',
  target_uid INT NULL DEFAULT NULL,
  operator VARCHAR(80) NOT NULL DEFAULT 'admin',
  summary VARCHAR(255) NOT NULL DEFAULT '',
  detail_json JSON NULL,
  ip VARCHAR(64) NULL DEFAULT '',
  user_agent VARCHAR(255) NULL DEFAULT '',
  create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  INDEX idx_module (module),
  INDEX idx_action (action),
  INDEX idx_target_uid (target_uid),
  INDEX idx_create_time (create_time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

#### `ai_chat_message`

用于 AI 助手会话历史。

```sql
CREATE TABLE IF NOT EXISTS ai_chat_message (
  id BIGINT NOT NULL AUTO_INCREMENT,
  session_id BIGINT NOT NULL DEFAULT 0,
  operator VARCHAR(80) NOT NULL DEFAULT 'admin',
  role VARCHAR(20) NOT NULL DEFAULT 'user',
  content MEDIUMTEXT NULL,
  action_json JSON NULL,
  result_json JSON NULL,
  create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  INDEX idx_operator_time (operator, create_time),
  INDEX idx_create_time (create_time),
  INDEX idx_session_time (session_id, create_time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

---

## 6. 配置说明

### 6.1 `config.yaml`

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

ai:
  base_url: https://api.deepseek.com
  api_key: your_ai_api_key
  model: deepseek-chat
```

### 6.2 配置项说明

| 配置项 | 说明 |
| --- | --- |
| `app.addr` | 后台监听地址，例如 `0.0.0.0:9100` |
| `database.host` | MySQL 地址 |
| `database.port` | MySQL 端口 |
| `database.name` | 数据库名称 |
| `database.user` | 数据库用户名 |
| `database.password` | 数据库密码 |
| `security.session_key` | Session Cookie 名称或密钥标识 |
| `security.delete_password` | 删除等高危操作的二级验证密码 |
| `ai.base_url` | OpenAI Compatible API 地址 |
| `ai.api_key` | AI 服务 API Key |
| `ai.model` | AI 模型名称 |

### 6.3 环境变量覆盖

服务支持通过环境变量覆盖部分配置，适合生产部署：

| 环境变量 | 对应配置 |
| --- | --- |
| `APP_ADDR` | `app.addr` |
| `DB_HOST` | `database.host` |
| `DB_PORT` | `database.port` |
| `DB_NAME` | `database.name` |
| `DB_USER` | `database.user` |
| `DB_PASSWORD` | `database.password` |

生产环境建议不要把真实数据库密码和 AI Key 提交到 Git 仓库，应通过服务器配置文件、环境变量或密钥管理服务注入。

---

## 7. 本地运行

### 7.1 安装依赖

```bash
go mod download
```

### 7.2 启动项目

```bash
go run .
```

启动后访问：

```text
http://127.0.0.1:9100
```

如果配置为 `0.0.0.0:9100`，局域网或服务器公网环境可通过对应 IP 访问。

### 7.3 Windows 编译

```powershell
go build -o fool_chat_admin_go.exe .
```

### 7.4 Linux 交叉编译

```powershell
$env:GOOS="linux"
$env:GOARCH="amd64"
go build -o fool_chat_admin_go .
```

编译完成后，需要上传：

```text
fool_chat_admin_go
templates/
static/
config.yaml
```

---

## 8. Linux 部署

### 8.1 推荐目录

```text
/opt/fool_chat_admin_go/
├── fool_chat_admin_go
├── config.yaml
├── templates/
└── static/
```

### 8.2 上传命令示例

```bash
scp ./fool_chat_admin_go root@your_server:/opt/fool_chat_admin_go/
scp ./config.yaml root@your_server:/opt/fool_chat_admin_go/
scp -r ./templates root@your_server:/opt/fool_chat_admin_go/
scp -r ./static root@your_server:/opt/fool_chat_admin_go/
```

### 8.3 手动启动

```bash
cd /opt/fool_chat_admin_go
chmod +x fool_chat_admin_go
nohup ./fool_chat_admin_go > app.log 2>&1 &
```

查看日志：

```bash
tail -f /opt/fool_chat_admin_go/app.log
```

停止进程：

```bash
pkill fool_chat_admin_go
```

### 8.4 systemd 托管

推荐生产环境使用 systemd 托管：

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

部署服务文件：

```bash
sudo cp fool-chat-admin.service /etc/systemd/system/fool-chat-admin.service
sudo systemctl daemon-reload
sudo systemctl enable --now fool-chat-admin
sudo systemctl status fool-chat-admin
```

更新后重启：

```bash
sudo systemctl restart fool-chat-admin
```

实时查看日志：

```bash
journalctl -u fool-chat-admin -f
```

### 8.5 防火墙

如果监听端口是 `9100`：

```bash
sudo ufw allow 9100/tcp
```

云服务器还需要在安全组中放行对应端口。

---

## 9. API 概览

所有 `/api/*` 接口除 `/api/login` 和 `/api/logout` 外均需要登录态。

### 9.1 认证接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/login` | 管理员登录 |
| `POST` | `/api/logout` | 退出登录 |

### 9.2 概览与日志

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/summary` | 概览指标 |
| `GET` | `/api/analytics` | 趋势统计 |
| `GET` | `/api/logs` | 操作日志列表 |
| `GET` | `/api/log-operators` | 日志操作人列表 |
| `POST` | `/api/get-bg-url` | 获取壁纸地址或代理图片数据 |

### 9.3 用户接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/users` | 用户列表 |
| `POST` | `/api/users` | 创建用户 |
| `PATCH` | `/api/users/{uid}` | 编辑用户资料 |
| `PATCH` | `/api/users/{uid}/password` | 重置密码 |
| `PATCH` | `/api/users/{uid}/role` | 任命或取消管理员 |
| `DELETE` | `/api/users/{uid}` | 删除用户，需要二级密码 |

### 9.4 动态接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/dynamics` | 动态列表 |
| `POST` | `/api/dynamics` | 发布动态 |
| `PATCH` | `/api/dynamics/{id}` | 编辑动态 |
| `PATCH` | `/api/dynamics/{id}/status` | 修改动态状态 |
| `DELETE` | `/api/dynamics/{id}` | 删除动态 |

### 9.5 好友与通知接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/friend-applies` | 好友申请列表 |
| `PATCH` | `/api/friend-applies/{id}` | 处理好友申请 |
| `GET` | `/api/friends` | 好友关系列表 |
| `DELETE` | `/api/friends/{self_id}/{friend_id}` | 删除好友关系 |
| `GET` | `/api/star-notices` | 公告列表 |
| `POST` | `/api/star-notices` | 新增公告 |
| `PATCH` | `/api/star-notices` | 编辑公告 |
| `DELETE` | `/api/star-notices` | 删除公告 |
| `GET` | `/api/admin-notices` | 后台通知列表 |
| `POST` | `/api/admin-notices` | 创建后台通知 |
| `PATCH` | `/api/admin-notices/{id}` | 编辑后台通知 |
| `PATCH` | `/api/admin-notices/{id}/delivered` | 标记通知已处理 |
| `DELETE` | `/api/admin-notices/{id}` | 删除后台通知 |

### 9.6 AI 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/ai/chat` | AI 对话与动作调度 |
| `GET` | `/api/ai/sessions` | AI 会话列表 |
| `GET` | `/api/ai/sessions/{id}` | AI 会话详情 |
| `DELETE` | `/api/ai/sessions/{id}` | 删除 AI 会话 |

---

## 10. 安全策略

### 10.1 权限控制

- 后台只允许管理员和超级管理员登录。
- 普通用户不能登录后台。
- 管理员不能操作权限高于或等于自己的账号。
- 管理员不能修改自己的角色。
- 管理员不能删除自己的账号。

### 10.2 高危操作保护

以下操作建议统一走确认弹窗和日志记录：

- 删除用户。
- 批量删除用户。
- 删除动态。
- 批量隐藏动态。
- 任命或取消管理员。
- AI 发起的删除、审核、通知投递、退出登录等动作。

删除用户额外要求二级验证密码。

### 10.3 日志审计

每条关键操作应记录：

- 操作模块。
- 操作动作。
- 目标类型和目标 ID。
- 操作人。
- 摘要说明。
- 详细 JSON。
- 操作 IP。
- 浏览器 User-Agent。
- 操作时间。

---

## 11. 运维建议

### 11.1 生产配置建议

- 使用 `0.0.0.0:9100` 监听服务端口。
- 使用 Nginx 反向代理并开启 HTTPS。
- 不要将数据库密码和 AI API Key 提交到仓库。
- MySQL 只开放必要来源 IP。
- 生产环境建议使用 systemd 托管，避免手动 nohup 难以追踪状态。
- 定期备份 MySQL 数据库。
- 定期导出或归档 `admin_operation_log`。

### 11.2 常见问题

#### 登录页面打不开

检查服务是否启动：

```bash
ps aux | grep fool_chat_admin_go
```

检查端口是否监听：

```bash
ss -lntp | grep 9100
```

#### 服务器运行后外网访问不了

需要同时检查：

- Go 服务是否监听 `0.0.0.0:9100`。
- Linux 防火墙是否放行端口。
- 云服务器安全组是否放行端口。

#### 时间显示不正确

如果 MySQL 运行在 Docker 中，容器时区可能是 UTC。可以检查：

```sql
SELECT NOW(), @@global.time_zone, @@session.time_zone, @@system_time_zone;
```

建议保证宿主机、MySQL 容器和应用服务时区一致。

#### AI 无法回答

重点检查：

- `config.yaml` 中 `ai.api_key` 是否配置。
- `ai.base_url` 是否是 OpenAI Compatible 地址。
- `ai.model` 是否是当前平台支持的模型名。
- 服务器是否能访问 AI 服务地址。
- 后端日志中是否有 AI 请求失败响应。

---

## 12. 版本交付清单

每次发布到服务器前，建议确认以下文件：

```text
fool_chat_admin_go          # Linux 可执行文件
templates/index.html        # 页面模板
static/app.js               # 前端逻辑
static/app.css              # 页面样式
static/icon.png             # 图标资源
static/xlsx.full.min.js     # Excel 导出依赖
config.yaml                 # 服务器运行配置
```

如果只修改了前端页面或样式，通常上传 `templates/` 和 `static/` 即可；如果修改了 Go 文件，必须重新编译并上传新的二进制文件。

---

## 13. 编译与发布命令参考

Windows 交叉编译 Linux：

```powershell
cd D:\study\code\fool_chat_admin_go
$env:GOOS="linux"
$env:GOARCH="amd64"
go build -o fool_chat_admin_go .
```

上传到服务器：

```bash
scp ./fool_chat_admin_go root@your_server:/opt/fool_chat_admin_go/
scp -r ./templates root@your_server:/opt/fool_chat_admin_go/
scp -r ./static root@your_server:/opt/fool_chat_admin_go/
```

服务器重启服务：

```bash
cd /opt/fool_chat_admin_go
chmod +x fool_chat_admin_go
pkill fool_chat_admin_go
nohup ./fool_chat_admin_go > app.log 2>&1 &
```

如果使用 systemd：

```bash
sudo systemctl restart fool-chat-admin
journalctl -u fool-chat-admin -f
```

---

## 14. 后续优化方向

- 接入更完整的 RBAC 权限模型，支持菜单级、按钮级权限。
- 将 Session 存储迁移到 Redis，支持多实例部署。
- 增加数据库迁移工具，替代启动时自动建表。
- 增加操作日志归档策略。
- 增加 AI 操作审批流。
- 增加通知投递状态与客户端回执统计。
- 增加后台单元测试和接口测试。
- 增加 Nginx + HTTPS 标准部署文档。

---

## 15. 项目总结

Fool Chat Admin 以轻量化部署为前提，为 Fool Chat 即时通讯系统补齐了后台运营和管理能力。项目覆盖账号权限、内容审核、通知触达、日志审计、数据统计和 AI 辅助操作等后台核心场景，能够作为即时通讯系统的管理中枢，也能作为毕业设计或简历项目中体现全栈能力、权限设计、工程部署和 AI 工具化能力的重要组成部分。