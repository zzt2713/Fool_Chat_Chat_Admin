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

## 安全 / 仓库

- 公开仓库：https://github.com/zzt2713/Fool_Chat_Chat_Admin
- `config.yaml` 已 gitignored（含 DB 密码、二级密码、AI Key），提交模板用 `config.example.yaml`
- 别把真实密钥写进任何提交文件、日志、commit message

## 沟通偏好

- 中文回复，结论先行，少废话
- 涉及生产服务器、git push、密钥/CI 配置改动 → 给命令让用户自己执行，不要代跑
- 改完代码默认不 commit，让用户自己 commit
