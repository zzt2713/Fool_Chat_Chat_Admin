# Fool Chat Admin Go

Go 写的单体后台管理系统：管理用户、动态、好友关系、公告、通知，带 AI 助手对话。

## 部署

```bash
# 编译 Linux 二进制
GOOS=linux GOARCH=amd64 go build -o fool_chat_admin_go .

# 上传到服务器
scp ./fool_chat_admin_go root@39.104.80.114:/opt/fool_chat_admin_go/fool_chat_admin_go

# 重启服务
ssh root@39.104.80.114 "systemctl restart fool_chat_admin_go"
```

生产部署/scp/重启相关命令**请发给用户自己跑**，不要代跑。

## 本地开发

```bash
go run .
```

默认监听 `0.0.0.0:9100`，配置在 `config.yaml`。

## 项目结构

单包项目，所有 Go 文件在根目录：

| 文件 | 职责 |
|---|---|
| `main.go` | 入口；`a.api()` 是 path-prefix 集中分发器，所有 `/api/*` 路由都在这一个 switch 里 |
| `auth.go` | 登录、cookie session、权限校验 |
| `db.go` | MySQL 连接 |
| `config.go` | YAML 配置加载 |
| `log.go` | `logOperation()` 写审计日志到 `admin_operation_log` |
| `util.go` | 通用工具：`writeJSON` / `writeErr` / `decodeJSON` / `queryMaps` / `intArg` / `strArg` 等 |
| `handler_*.go` | 各功能 handler |
| `handler_password_reset.go` | 找回密码三步接口（lookup / send / reset） |
| `verify_client.go` | 调外部 gRPC 验证码服务（`VarifyService.GetVarifyCode`）的轻量 client |
| `templates/index.html` | 单页前端 |
| `static/app.js` | 原生 JS，无构建，无框架 |
| `static/app.css` | 样式 |

数据库名 `mhkh`，主要表：`user` / `dynamic` / `friend` / `friend_apply` / `StarNotice` / `admin_notice` / `admin_operation_log` / `ai_chat_message`。

## 惯用模式

**写数据库**：用裸 SQL（`a.db.Exec` / `a.db.Query` / `a.queryMaps`），不用 ORM。

**写响应**：成功 `writeJSON(w, data)`，失败 `writeErr(w, code, msg)`，不要自己拼 JSON。

**解请求体**：用 `decodeJSON(w, r, &p)`，失败它会自己写 400/401 并返回 false，外层只需 `if !decodeJSON(...) { return }`。

**记审计**：所有 **修改性**操作（增、删、改、发通知、改状态）都必须调用 `a.logOperation(r, operator, module, action, targetType, targetID, targetUID, summary, detail)`，否则后台日志页查不到。

**handler 签名**：登录后的接口签名一律是 `func (a *app) xxx(w http.ResponseWriter, r *http.Request, operator string)`。`operator` 由 `main.go:api()` 在分发前从 session 解出来传入。

**权限校验**：删用户、改用户角色等敏感操作用 `a.getOperatorAndTarget(operator, targetUID)` 拿到 `operatorUID, operatorRole, targetRole`，常见规则：
- 不能操作自己（`operatorUID == targetUID`）
- 不能操作权限 ≥ 自己的人（`targetRole >= operatorRole`）

**二级密码**：删用户级别的高危操作要校验 `cfg.DeletePassword`，前端会弹密码框收集。

## 找回密码（handler_password_reset.go）

登录页「忘记密码？」走三步：账号 → 邮箱验证码 → 新密码。三个**免登录**接口（`/api/password-reset/lookup|send|reset`），在 `main.go:api()` 里**必须放在 currentUser 校验之前**。

链路：
1. `lookup` 按 `user.name` 查 `email`、返回脱敏邮箱（`maskEmail` 保留前 2 字符）
2. `send` 通过 gRPC 调 VarifyServer 的 `GetVarifyCode(email)`；VarifyServer 自己把验证码写到 Redis `code_<email>`（TTL 600s）
3. `reset` 后端再去 Redis 读 `code_<email>` 校验（大小写无关，`EqualFold`），通过后 SHA256 重写 `user.pwd`，删 key，写审计

**Redis / 验证码 key 必须和 VarifyServer 端一致**：`code_<完整邮箱>`，前缀来自 `VarifyServer/const.js`。两边连同一个 Redis 实例，否则查不到。

## 验证码服务 VarifyServer（不属于本仓库）

- 源码：`D:\study\boostasio\VarifyServer`（本地 Windows 工作目录；Node.js gRPC + nodemailer）
- proto：`message.VarifyService.GetVarifyCode(email) → {error, email, code}`
- 部署：`/opt/VarifyServer`（39.104.80.114）；systemd unit `varify.service` 跑 `node server.js`，监听 `0.0.0.0:50051`
- 日志：`/var/log/varify.log`
- 阿里云安全组需放行 `50051/TCP`
- **不要改 VarifyServer 代码**——本仓库只负责调用它

config.yaml 相关字段：
```yaml
redis:
  host: 39.104.80.114
  port: "6380"
  password: "123456"
verify_server:
  addr: 39.104.80.114:50051
```
`config.go` 里有同样默认值兜底，没写也能跑。

## AI 助手（handler_ai.go）的特殊架构

AI 不直接写 SQL。它选一个**预定义动作**，参数填到 JSON 里，后端按动作名 switch 执行。

- 动作清单和 schema 写在 `decideAIAction()` 的 system prompt 里，**改动作必须同步改 prompt**
- 当 `cfg.AIAPIKey` 没配，自动走 `fallbackAIDecision()` 关键词兜底（保证离线可用）
- 动作分类：
  - `isClientOnlyAIAction()` —— 前端执行（壁纸、主题、导航、登出等），server 立刻返回不进 `executeAIAction`
  - `isHighRiskAIAction()` —— `requires_confirm=true`，前端弹确认框
  - **既高危又是前端动作**（如 `logout`）：在 `aiChat` 主流程里**先**判 high-risk，**后**判 client-only，确保走确认流程
- 前端 `handleClientAIAction()`（`static/app.js`）和后端 `isClientOnlyAIAction()` 列表必须保持一致

新增一个 AI 动作的清单：
1. `decideAIAction` 的 system prompt 里加描述
2. `fallbackAIDecision` 加关键词兜底
3. 如果是后端动作 → `executeAIAction` 加 case
4. 如果是前端动作 → `isClientOnlyAIAction` 加名字 + `static/app.js` 的 `handleClientAIAction` 加 case
5. 如果高危 → `isHighRiskAIAction` 加名字

## 前端约定（static/app.js）

- 全局状态：`currentView`（当前视图）、`pageState`（各列表分页）、`aiCurrentSessionId`
- 切视图：`activateView(view)`，视图名在文件顶部 `titles` 字典里
- 当前视图刷新：`refreshCurrent()`
- 统一请求：`api(path, opts)`，会处理 loading toast 和 401 跳登录
- 没有构建工具，**改完直接刷新浏览器**
- **改了 `static/*` 或 `templates/index.html` 记得 bump `index.html` 里的 `?v=` 版本号**，否则用户浏览器吃缓存

### 概览指标卡（dashboard）

新增/修改一张卡的清单：
1. `templates/index.html` 的 `.metrics` 里加 `<div class="metric"><span>标题</span><strong id="mXxx">-</strong></div>`
2. `handler_stats.go:summary` 的 SQL 加 `(SELECT ...) AS xxx` 列
3. `static/app.js:loadSummary` 里 `$("#mXxx").textContent = data.xxx ?? 0;`

> 当前指标里的「在线用户」= `SELECT COUNT(*) FROM user WHERE status = 1`（替换了原来的「待处理申请」）。AI 助手 `query_summary` 仍返回 `pending_applies` 字段，**别一起删掉**。

### AI 助手「可用能力」模板按钮

右侧能力面板里每个 `.ai-help-item` 带 `data-template="..."`，点击后：
1. 模板写入 `#aiInput`
2. 用正则 `/\[[^\]]+\]/` 匹配第一个 `[占位符]` 区段，`setSelectionRange` 选中它，让用户直接打字覆盖

加新能力按钮：HTML 里追加一个 `<button class="ai-help-item" data-template="...">标题</button>` 即可，**不需要改 JS**（事件是 `querySelectorAll(".ai-help-item")` 自动绑定的）。高危项追加 `ai-help-danger` 类，会变成整行红色样式。

### AI 输入框 / 发送按钮

- 输入框包了一层 `.ai-input-wrap`，`textarea { resize: none }`，**不要再开拖拽角**
- 发送按钮是蓝紫渐变，禁用态自动变灰（看 `.ai-input-row #aiSendBtn` 系列样式）
- `sendAIMessage` 的 `finally` 里会把焦点还给 `#aiInput`，**仅当**没有 `aiPendingAction`（高危确认中）且 `currentView === "ai"`

## 安全 / 仓库

- 公开仓库：https://github.com/zzt2713/Fool_Chat_Chat_Admin
- `config.yaml` 已 gitignored（含 DB 密码、二级密码、AI Key），提交模板用 `config.example.yaml`
- 别把真实密钥写进任何提交文件、日志、commit message

## 沟通偏好

- 中文回复，结论先行，少废话
- 涉及生产服务器、git push、密钥/CI 配置改动 → 给命令让用户自己执行，不要代跑
- 改完代码默认不 commit，让用户自己 commit
