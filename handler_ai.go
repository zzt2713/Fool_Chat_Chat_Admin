package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type aiChatPayload struct {
	Message        string    `json:"message"`
	SessionID      int64     `json:"session_id"`
	Confirm        bool      `json:"confirm"`
	DeletePassword string    `json:"delete_password"`
	PendingAction  *aiAction `json:"pending_action"`
}

type aiAction struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type aiDecision struct {
	Reply           string    `json:"reply"`
	Action          *aiAction `json:"action"`
	RequiresConfirm bool      `json:"requires_confirm"`
}

type aiChatResponse struct {
	Reply           string    `json:"reply"`
	Action          *aiAction `json:"action,omitempty"`
	RequiresConfirm bool      `json:"requires_confirm"`
	Result          any       `json:"result,omitempty"`
	SessionID       int64     `json:"session_id"`
}

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
}

func (a *app) aiChat(w http.ResponseWriter, r *http.Request, operator string) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	var p aiChatPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	p.Message = strings.TrimSpace(p.Message)
	if p.Message == "" && p.PendingAction == nil {
		writeErr(w, http.StatusBadRequest, "请输入要让 AI 助手处理的内容")
		return
	}

	if p.SessionID == 0 {
		p.SessionID = time.Now().UnixMilli()
	}
	if p.Message != "" {
		a.saveAIMessage(p.SessionID, operator, "user", p.Message, nil, nil)
	}

	var decision aiDecision
	var err error
	if p.PendingAction != nil {
		decision = aiDecision{Action: p.PendingAction, RequiresConfirm: isHighRiskAIAction(p.PendingAction.Name)}
	} else {
		history := a.loadAIHistory(p.SessionID, operator, 20)
		if len(history) > 0 && history[len(history)-1].Role == "user" && history[len(history)-1].Content == p.Message {
			history = history[:len(history)-1]
		}
		decision, err = a.decideAIAction(r.Context(), history, p.Message)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
	}

	if decision.Action == nil || decision.Action.Name == "" {
		if decision.Reply == "" {
			decision.Reply = "我可以帮你查询用户、查询动态、审核动态、隐藏动态、删除动态、发送通知、查询日志，也能切换壁纸和主题。高危操作会先让你确认。"
		}
		a.saveAIMessage(p.SessionID, operator, "assistant", decision.Reply, nil, nil)
		writeJSON(w, aiChatResponse{Reply: decision.Reply, SessionID: p.SessionID})
		return
	}

	if isClientOnlyAIAction(decision.Action.Name) {
		reply := decision.Reply
		if reply == "" {
			reply = "已操作。"
		}
		a.saveAIMessage(p.SessionID, operator, "assistant", reply, decision.Action, nil)
		writeJSON(w, aiChatResponse{Reply: reply, Action: decision.Action, SessionID: p.SessionID})
		return
	}

	if isHighRiskAIAction(decision.Action.Name) && !p.Confirm {
		reply := decision.Reply
		if reply == "" {
			reply = describeAIAction(decision.Action)
		}
		a.saveAIMessage(p.SessionID, operator, "assistant", reply, decision.Action, nil)
		writeJSON(w, aiChatResponse{Reply: reply, Action: decision.Action, RequiresConfirm: true, SessionID: p.SessionID})
		return
	}

	result, err := a.executeAIAction(r, operator, decision.Action, p.DeletePassword)
	if err != nil {
		a.saveAIMessage(p.SessionID, operator, "assistant", "执行失败："+err.Error(), decision.Action, nil)
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	reply := "已完成。"
	if decision.Reply != "" {
		reply = decision.Reply
	}
	a.saveAIMessage(p.SessionID, operator, "assistant", reply, decision.Action, result)
	writeJSON(w, aiChatResponse{Reply: reply, Action: decision.Action, Result: result, SessionID: p.SessionID})
}

func (a *app) decideAIAction(ctx context.Context, history []openAIMessage, message string) (aiDecision, error) {
	if a.cfg.AIAPIKey == "" {
		if d, ok := fallbackAIDecision(message); ok {
			return d, nil
		}
		return aiDecision{}, errors.New("AI API Key 未配置，请在 config.yaml 的 ai.api_key 中填写密钥")
	}
	baseURL := strings.TrimRight(a.cfg.AIBaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	chatURL := baseURL + "/v1/chat/completions"
	if strings.HasSuffix(baseURL, "/v1") {
		chatURL = baseURL + "/chat/completions"
	}
	model := a.cfg.AIModel
	if model == "" {
		model = "deepseek-chat"
	}

	sys := `你是 Fool Chat 后台管理助手。你不能直接写 SQL，只能从这些动作中选择一个并返回 JSON：
query_user {"uid":数字? , "q":"关键词"?}
query_dynamic {"id":数字? , "q":"关键词"?}
update_dynamic_status {"id":数字, "status":0|1|2}，0通过/正常，1审核中，2隐藏
send_notice {"target_uid":数字或null, "title":"标题", "content":"内容", "level":"info|success|warning|error"}
query_logs {"q":"关键词"?}
delete_dynamic {"id":数字}
delete_user {"uid":数字}
switch_wallpaper {} - 随机切换网页背景壁纸
set_theme {"mode":"dark"|"light"|"toggle"} - 切换主题，dark 暗黑 / light 亮色 / toggle 反转
如果用户只是咨询，就 action=null。删除用户、删除动态、修改动态状态、发送通知都 requires_confirm=true。switch_wallpaper 和 set_theme 是页面操作，requires_confirm=false。只返回 JSON，不要 Markdown。格式：{"reply":"给管理员看的中文说明","action":{"name":"动作名","args":{}},"requires_confirm":true或false}`

	messages := []openAIMessage{{Role: "system", Content: sys}}
	messages = append(messages, history...)
	messages = append(messages, openAIMessage{Role: "user", Content: message})
	reqBody := openAIChatRequest{
		Model:       model,
		Temperature: 0.1,
		Messages:    messages,
	}
	b, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatURL, bytes.NewReader(b))
	if err != nil {
		return aiDecision{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.AIAPIKey)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return aiDecision{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return aiDecision{}, fmt.Errorf("AI 请求失败：%s", strings.TrimSpace(string(body)))
	}
	var out openAIChatResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return aiDecision{}, err
	}
	if len(out.Choices) == 0 {
		return aiDecision{}, errors.New("AI 没有返回内容")
	}
	content := strings.TrimSpace(out.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	var decision aiDecision
	if err := json.Unmarshal([]byte(content), &decision); err != nil {
		return aiDecision{Reply: content}, nil
	}
	return decision, nil
}

func fallbackAIDecision(message string) (aiDecision, bool) {
	msg := strings.TrimSpace(message)
	id := firstInt(msg)
	switch {
	case strings.Contains(msg, "切换壁纸") || strings.Contains(msg, "换壁纸") || strings.Contains(msg, "换背景") || strings.Contains(msg, "换张壁纸"):
		return aiDecision{Reply: "已切换壁纸。", Action: &aiAction{Name: "switch_wallpaper", Args: map[string]any{}}}, true
	case strings.Contains(msg, "暗黑") || strings.Contains(msg, "深色") || strings.Contains(msg, "夜间") || strings.Contains(msg, "黑暗模式"):
		return aiDecision{Reply: "已切换为暗黑模式。", Action: &aiAction{Name: "set_theme", Args: map[string]any{"mode": "dark"}}}, true
	case strings.Contains(msg, "亮色") || strings.Contains(msg, "浅色") || strings.Contains(msg, "白天") || strings.Contains(msg, "明亮模式"):
		return aiDecision{Reply: "已切换为亮色模式。", Action: &aiAction{Name: "set_theme", Args: map[string]any{"mode": "light"}}}, true
	case strings.Contains(msg, "切换主题") || strings.Contains(msg, "换主题") || strings.Contains(msg, "切换模式"):
		return aiDecision{Reply: "已切换主题。", Action: &aiAction{Name: "set_theme", Args: map[string]any{"mode": "toggle"}}}, true
	case strings.Contains(msg, "删除用户") && id > 0:
		return aiDecision{Reply: fmt.Sprintf("将删除 UID %d 用户，并清理相关动态、好友关系和通知。请确认并输入二级密码。", id), Action: &aiAction{Name: "delete_user", Args: map[string]any{"uid": id}}, RequiresConfirm: true}, true
	case strings.Contains(msg, "删除动态") && id > 0:
		return aiDecision{Reply: fmt.Sprintf("将删除动态 ID %d，请确认。", id), Action: &aiAction{Name: "delete_dynamic", Args: map[string]any{"id": id}}, RequiresConfirm: true}, true
	case (strings.Contains(msg, "隐藏动态") || strings.Contains(msg, "违规")) && id > 0:
		return aiDecision{Reply: fmt.Sprintf("将把动态 ID %d 设置为隐藏。", id), Action: &aiAction{Name: "update_dynamic_status", Args: map[string]any{"id": id, "status": 2}}, RequiresConfirm: true}, true
	case (strings.Contains(msg, "通过动态") || strings.Contains(msg, "审核通过")) && id > 0:
		return aiDecision{Reply: fmt.Sprintf("将把动态 ID %d 设置为正常。", id), Action: &aiAction{Name: "update_dynamic_status", Args: map[string]any{"id": id, "status": 0}}, RequiresConfirm: true}, true
	case strings.Contains(msg, "查询用户") || strings.Contains(msg, "查用户"):
		args := map[string]any{}
		if id > 0 {
			args["uid"] = id
		} else {
			args["q"] = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(msg, "查询用户"), "查用户"))
		}
		return aiDecision{Reply: "查询用户结果如下。", Action: &aiAction{Name: "query_user", Args: args}}, true
	case strings.Contains(msg, "查询动态") || strings.Contains(msg, "查动态"):
		args := map[string]any{}
		if id > 0 {
			args["id"] = id
		} else {
			args["q"] = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(msg, "查询动态"), "查动态"))
		}
		return aiDecision{Reply: "查询动态结果如下。", Action: &aiAction{Name: "query_dynamic", Args: args}}, true
	}
	return aiDecision{}, false
}

func firstInt(s string) int {
	cur := ""
	for _, r := range s {
		if r >= '0' && r <= '9' {
			cur += string(r)
			continue
		}
		if cur != "" {
			break
		}
	}
	n, _ := strconv.Atoi(cur)
	return n
}

func isClientOnlyAIAction(name string) bool {
	switch name {
	case "switch_wallpaper", "set_theme":
		return true
	default:
		return false
	}
}

func isHighRiskAIAction(name string) bool {
	switch name {
	case "delete_user", "delete_dynamic", "update_dynamic_status", "send_notice":
		return true
	default:
		return false
	}
}

func describeAIAction(action *aiAction) string {
	if action == nil {
		return ""
	}
	b, _ := json.Marshal(action.Args)
	return fmt.Sprintf("准备执行 %s，参数：%s。请确认。", action.Name, string(b))
}

func (a *app) executeAIAction(r *http.Request, operator string, action *aiAction, deletePassword string) (any, error) {
	switch action.Name {
	case "query_user":
		uid := intArg(action.Args, "uid")
		if uid > 0 {
			rows, err := a.queryMaps("SELECT uid, name, email, nick, `desc`, sex, icon, role FROM `user` WHERE uid = ? LIMIT 1", uid)
			return rows, err
		}
		q := strings.TrimSpace(strArg(action.Args, "q"))
		kw := "%" + q + "%"
		return a.queryMaps("SELECT uid, name, email, nick, `desc`, sex, icon, role FROM `user` WHERE name LIKE ? OR email LIKE ? OR nick LIKE ? OR CAST(uid AS CHAR) LIKE ? ORDER BY uid ASC LIMIT 20", kw, kw, kw, kw)
	case "query_dynamic":
		id := intArg(action.Args, "id")
		if id > 0 {
			return a.queryMaps("SELECT d.id, d.uid, u.name, d.content, d.like_count, d.status, d.create_time FROM `dynamic` d LEFT JOIN `user` u ON d.uid = u.uid WHERE d.id = ? LIMIT 1", id)
		}
		q := strings.TrimSpace(strArg(action.Args, "q"))
		kw := "%" + q + "%"
		return a.queryMaps("SELECT d.id, d.uid, u.name, d.content, d.like_count, d.status, d.create_time FROM `dynamic` d LEFT JOIN `user` u ON d.uid = u.uid WHERE u.name LIKE ? OR d.content LIKE ? OR CAST(d.uid AS CHAR) LIKE ? ORDER BY d.id DESC LIMIT 20", kw, kw, kw)
	case "query_logs":
		q := strings.TrimSpace(strArg(action.Args, "q"))
		kw := "%" + q + "%"
		return a.queryMaps("SELECT id, module, action, summary, operator, create_time FROM admin_operation_log WHERE summary LIKE ? OR operator LIKE ? OR module LIKE ? ORDER BY id DESC LIMIT 20", kw, kw, kw)
	case "update_dynamic_status":
		id := intArg(action.Args, "id")
		status := intArg(action.Args, "status")
		if id <= 0 || status < 0 || status > 2 {
			return nil, errors.New("动态 ID 或状态参数不正确")
		}
		res, err := a.db.Exec("UPDATE `dynamic` SET status = ? WHERE id = ?", status, id)
		if err != nil {
			return nil, err
		}
		a.logOperation(r, operator, "ai", "update_dynamic_status", "dynamic", strconv.Itoa(id), nil, fmt.Sprintf("AI 修改动态 ID %d 状态为 %d", id, status), action.Args)
		return map[string]any{"ok": affected(res) > 0}, nil
	case "delete_dynamic":
		id := intArg(action.Args, "id")
		if id <= 0 {
			return nil, errors.New("动态 ID 不正确")
		}
		res, err := a.db.Exec("DELETE FROM `dynamic` WHERE id = ?", id)
		if err != nil {
			return nil, err
		}
		a.logOperation(r, operator, "ai", "delete_dynamic", "dynamic", strconv.Itoa(id), nil, fmt.Sprintf("AI 删除动态 ID %d", id), action.Args)
		return map[string]any{"ok": affected(res) > 0}, nil
	case "send_notice":
		title := strings.TrimSpace(strArg(action.Args, "title"))
		content := strings.TrimSpace(strArg(action.Args, "content"))
		level := strings.TrimSpace(strArg(action.Args, "level"))
		if level == "" {
			level = "info"
		}
		if title == "" || content == "" {
			return nil, errors.New("通知标题和内容不能为空")
		}
		var target any = nil
		if uid := intArg(action.Args, "target_uid"); uid > 0 {
			target = uid
		}
		res, err := a.db.Exec("INSERT INTO admin_notice(target_uid, title, content, level) VALUES (?, ?, ?, ?)", target, title, content, level)
		if err != nil {
			return nil, err
		}
		id := int(lastID(res))
		a.logOperation(r, operator, "ai", "send_notice", "admin_notice", strconv.Itoa(id), nil, fmt.Sprintf("AI 创建通知 ID %d", id), action.Args)
		return map[string]any{"ok": true, "id": id}, nil
	case "delete_user":
		uid := intArg(action.Args, "uid")
		if uid <= 0 {
			return nil, errors.New("用户 UID 不正确")
		}
		if deletePassword != a.cfg.DeletePassword {
			return nil, errors.New("二级验证密码错误")
		}
		operatorUID, operatorRole, targetRole, err := a.getOperatorAndTarget(operator, uid)
		if err != nil {
			return nil, errors.New("用户不存在")
		}
		if operatorUID == uid {
			return nil, errors.New("不能删除自己的账号")
		}
		if targetRole >= operatorRole {
			return nil, errors.New("不能删除权限高于或等于自己的账号")
		}
		tx, err := a.db.Begin()
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		stmts := []string{
			"DELETE FROM friend_apply WHERE from_uid = ? OR to_uid = ?",
			"DELETE FROM friend WHERE self_id = ? OR friend_id = ?",
			"DELETE FROM `dynamic` WHERE uid = ?",
			"DELETE FROM admin_notice WHERE target_uid = ?",
			"DELETE FROM `user` WHERE uid = ?",
		}
		for i, stmt := range stmts {
			if i < 2 {
				_, err = tx.Exec(stmt, uid, uid)
			} else {
				_, err = tx.Exec(stmt, uid)
			}
			if err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		a.logOperation(r, operator, "ai", "delete_user", "user", strconv.Itoa(uid), &uid, fmt.Sprintf("AI 删除账号 UID %d", uid), action.Args)
		return map[string]any{"ok": true}, nil
	default:
		return nil, errors.New("AI 动作不支持：" + action.Name)
	}
}

func intArg(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func strArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	switch v := args[key].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

var _ = sql.ErrNoRows

func (a *app) saveAIMessage(sessionID int64, operator, role, content string, action *aiAction, result any) {
	if sessionID == 0 {
		return
	}
	var actionVal, resultVal any
	if action != nil {
		b, _ := json.Marshal(action)
		actionVal = string(b)
	}
	if result != nil {
		b, _ := json.Marshal(result)
		resultVal = string(b)
	}
	_, _ = a.db.Exec(
		"INSERT INTO ai_chat_message (session_id, operator, role, content, action_json, result_json) VALUES (?, ?, ?, ?, ?, ?)",
		sessionID, operator, role, content, actionVal, resultVal,
	)
}

func (a *app) loadAIHistory(sessionID int64, operator string, limit int) []openAIMessage {
	if sessionID == 0 {
		return nil
	}
	rows, err := a.db.Query(
		"SELECT role, content FROM ai_chat_message WHERE session_id = ? AND operator = ? AND role IN ('user','assistant') ORDER BY id DESC LIMIT ?",
		sessionID, operator, limit,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []openAIMessage
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err == nil {
			out = append(out, openAIMessage{Role: role, Content: content})
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (a *app) aiSessions(w http.ResponseWriter, r *http.Request, operator string) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	rows, err := a.queryMaps(`
		SELECT
			m.session_id,
			MAX(m.create_time) AS last_time,
			COUNT(*) AS msg_count,
			(SELECT content FROM ai_chat_message WHERE session_id = m.session_id AND role = 'user' ORDER BY id ASC LIMIT 1) AS title
		FROM ai_chat_message m
		WHERE m.operator = ? AND m.session_id > 0
		GROUP BY m.session_id
		ORDER BY last_time DESC
		LIMIT 50`, operator)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, rows)
}

func (a *app) aiSessionByID(w http.ResponseWriter, r *http.Request, operator string) {
	raw := strings.TrimPrefix(r.URL.Path, "/api/ai/sessions/")
	parts := strings.SplitN(raw, "/", 2)
	if parts[0] == "" {
		writeErr(w, 404, "会话不存在")
		return
	}
	sessionID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || sessionID <= 0 {
		writeErr(w, 400, "会话 ID 无效")
		return
	}
	rest := ""
	if len(parts) > 1 {
		rest = parts[1]
	}

	switch r.Method {
	case http.MethodGet:
		if rest != "messages" && rest != "" {
			writeErr(w, 404, "路径不存在")
			return
		}
		rows, err := a.queryMaps(
			"SELECT role, content, action_json, result_json, create_time FROM ai_chat_message WHERE session_id = ? AND operator = ? ORDER BY id ASC",
			sessionID, operator,
		)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, rows)
	case http.MethodDelete:
		_, err := a.db.Exec("DELETE FROM ai_chat_message WHERE session_id = ? AND operator = ?", sessionID, operator)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		writeErr(w, 405, "方法不支持")
	}
}
