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
			decision.Reply = "我可以帮你查询用户/动态/日志/好友申请/公告/概览、聚合搜索、审核或批量隐藏动态、删改用户、发送通知；也能切换/下载/上传/预览壁纸、切主题、跳转页面、刷新数据、退出登录。高危操作会先让你确认。"
		}
		a.saveAIMessage(p.SessionID, operator, "assistant", decision.Reply, nil, nil)
		writeJSON(w, aiChatResponse{Reply: decision.Reply, SessionID: p.SessionID})
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

	if isClientOnlyAIAction(decision.Action.Name) {
		reply := decision.Reply
		if reply == "" {
			reply = "已操作。"
		}
		a.saveAIMessage(p.SessionID, operator, "assistant", reply, decision.Action, nil)
		writeJSON(w, aiChatResponse{Reply: reply, Action: decision.Action, SessionID: p.SessionID})
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
query_notices {"q":"关键词"?, "level":"info|success|warning|error"?, "delivered":0|1?} - 查询后台通知投递
query_friends {"q":"关键词"?} - 查询好友关系
query_online_users {} - 查询当前在线用户
query_recent_users {"limit":数字?} - 查询最近注册/UID 较新的用户
query_pending_dynamics {} - 查询待审核动态
query_admins {} - 查询管理员账号
delete_dynamic {"id":数字}
delete_user {"uid":数字}
switch_wallpaper {} - 随机切换网页背景壁纸
set_theme {"mode":"dark"|"light"|"toggle"} - 切换主题，dark 暗黑 / light 亮色 / toggle 反转
download_wallpaper {} - 下载当前壁纸到本地
upload_wallpaper {} - 打开本地图片选择器上传自定义壁纸
toggle_bg_preview {} - 切换壁纸预览模式（沉浸/退出）
navigate {"view":"dashboard|users|dynamics|friends|applies|star|notices|ai"} - 跳转到指定页面
refresh_view {} - 刷新当前页面数据
logout {} - 退出登录
query_summary {} - 查看后台概览（用户数、动态数、今日新增、待审等）
query_friend_applies {"q":"关键词"?, "status":0|1|2?} - 查询好友申请，status 0待处理 1已通过 2已拒绝
query_star_notices {"q":"关键词"?} - 查询 StarNotice 公告列表
search_all {"q":"关键词"} - 跨用户/动态/日志/通知/公告聚合搜索
batch_hide_dynamics_by_keyword {"q":"关键词"} - 批量隐藏所有内容含关键词的动态（高危）
update_user_role {"uid":数字, "role":数字} - 修改用户角色，role 越大权限越高（高危）
如果用户只是咨询，就 action=null。尽量给出简短中文回复，不要照搬数据库字段名。delete_user、delete_dynamic、update_dynamic_status、send_notice、logout、batch_hide_dynamics_by_keyword、update_user_role 都 requires_confirm=true。其余页面操作和查询类 requires_confirm=false。只返回 JSON，不要 Markdown。格式：{"reply":"给管理员看的中文说明","action":{"name":"动作名","args":{}},"requires_confirm":true或false}`

	sys += `
补充动作：
create_star_notice {"title":"公告标题", "content":"公告内容", "author":"作者，可选"} - 生成并发布公告
query_today_errors {} - 查询今天异常操作
group_pending_dynamics_by_uid {} - 把待审核动态按 UID 分组
operation_report {} - 生成今日运营日报
set_wallpaper_url {"url":"图片 URL"} - 使用网络图片 URL 作为背景
clear_wallpaper {} - 清除当前自定义背景
navigate 支持 view=dashboard|monitor|maintenance|users|dynamics|friends|applies|star|notices|ai
auto_review_dynamics {"limit":数字?最大50} - 用 AI 批量审核 status=1 的待审动态，违规设为隐藏(2)、合规设为通过(0)。需确认（高危）。
create_star_notice 和 auto_review_dynamics 需要确认，其余新增查询和页面动作不需要确认。
`
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

func (a *app) reviewDynamicsWithAI(ctx context.Context, items []map[string]any) ([]map[string]any, error) {
	if a.cfg.AIAPIKey == "" {
		return nil, errors.New("AI API Key 未配置，无法自动审批")
	}
	if len(items) == 0 {
		return nil, nil
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
	sys := `你是社区内容审核员。我会给你一批用户动态，请逐条判断是否违规并给出处置。
违规标准：色情、淫秽、暴力血腥、政治敏感、人身攻击/辱骂、广告引流推广、欺诈、违法犯罪、垃圾刷屏。
合规标准：日常生活、情感分享、正常评论、合理表达、对话片段。
返回严格 JSON 数组，每项 {"id": 数字, "verdict": "approve" 或 "hide", "reason": "中文简短理由不超过30字"}。
只输出 JSON 数组本身，不要 Markdown、不要解释、不要包裹任何代码块。`
	var sb strings.Builder
	sb.WriteString("待审动态如下，请逐条返回判定：\n")
	for _, it := range items {
		id := intArg(it, "id")
		content := strArg(it, "content")
		if len([]rune(content)) > 400 {
			rs := []rune(content)
			content = string(rs[:400]) + "..."
		}
		sb.WriteString(fmt.Sprintf("id=%d 内容=%q\n", id, content))
	}
	reqBody := openAIChatRequest{
		Model:       model,
		Temperature: 0,
		Messages: []openAIMessage{
			{Role: "system", Content: sys},
			{Role: "user", Content: sb.String()},
		},
	}
	b, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatURL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.AIAPIKey)
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("AI 请求失败：%s", strings.TrimSpace(string(body)))
	}
	var out openAIChatResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, errors.New("AI 没有返回内容")
	}
	content := strings.TrimSpace(out.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	if i := strings.Index(content, "["); i > 0 {
		content = content[i:]
	}
	if j := strings.LastIndex(content, "]"); j >= 0 && j < len(content)-1 {
		content = content[:j+1]
	}
	var verdicts []map[string]any
	if err := json.Unmarshal([]byte(content), &verdicts); err != nil {
		return nil, fmt.Errorf("AI 返回解析失败：%v", err)
	}
	return verdicts, nil
}

func (a *app) runAutoReviewDynamics(r *http.Request, operator string, limit int) (any, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	pending, err := a.queryMaps(`SELECT d.id, d.uid, u.name, d.content
		FROM `+"`dynamic`"+` d LEFT JOIN `+"`user`"+` u ON d.uid = u.uid
		WHERE d.status = 1 ORDER BY d.id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	if len(pending) == 0 {
		return map[string]any{
			"summary": map[string]any{"reviewed": 0, "approved_count": 0, "hidden_count": 0, "skipped_count": 0},
			"details": []any{},
		}, nil
	}
	verdicts, err := a.reviewDynamicsWithAI(r.Context(), pending)
	if err != nil {
		return nil, err
	}
	verdictByID := map[int]map[string]any{}
	for _, v := range verdicts {
		id := intArg(v, "id")
		if id > 0 {
			verdictByID[id] = v
		}
	}
	var approved, hidden, skipped int
	details := make([]map[string]any, 0, len(pending))
	for _, p := range pending {
		id := intArg(p, "id")
		v, ok := verdictByID[id]
		if !ok {
			skipped++
			details = append(details, map[string]any{
				"id": id, "uid": p["uid"], "name": p["name"],
				"verdict": "skip", "reason": "AI 未返回该条结果",
			})
			continue
		}
		verdict := strings.ToLower(strings.TrimSpace(strArg(v, "verdict")))
		reason := strArg(v, "reason")
		var newStatus int
		switch verdict {
		case "hide", "violate", "violation", "reject", "block":
			newStatus = 2
			verdict = "hide"
		case "approve", "pass", "ok", "allow":
			newStatus = 0
			verdict = "approve"
		default:
			skipped++
			details = append(details, map[string]any{
				"id": id, "uid": p["uid"], "name": p["name"],
				"verdict": "skip", "reason": "未知判定：" + reason,
			})
			continue
		}
		if _, err := a.db.Exec("UPDATE `dynamic` SET status = ? WHERE id = ?", newStatus, id); err != nil {
			skipped++
			details = append(details, map[string]any{
				"id": id, "uid": p["uid"], "name": p["name"],
				"verdict": "skip", "reason": "写库失败：" + err.Error(),
			})
			continue
		}
		if newStatus == 2 {
			hidden++
		} else {
			approved++
		}
		details = append(details, map[string]any{
			"id": id, "uid": p["uid"], "name": p["name"],
			"verdict": verdict, "reason": reason,
		})
	}
	a.logOperation(r, operator, "ai", "auto_review_dynamics", "dynamic", "", nil,
		fmt.Sprintf("AI 自动审批 %d 条：通过 %d，隐藏 %d，跳过 %d", len(pending), approved, hidden, skipped),
		map[string]any{"approved": approved, "hidden": hidden, "skipped": skipped, "details": details})
	return map[string]any{
		"summary": map[string]any{
			"reviewed":       len(pending),
			"approved_count": approved,
			"hidden_count":   hidden,
			"skipped_count":  skipped,
		},
		"details": details,
	}, nil
}

func fallbackAIDecision(message string) (aiDecision, bool) {
	msg := strings.TrimSpace(message)
	id := firstInt(msg)
	if strings.Contains(msg, "生成一条公告") || strings.Contains(msg, "生成公告") || strings.Contains(msg, "新增公告") || strings.Contains(msg, "发布公告") {
		title, content := parseNoticeLikeText(msg)
		if title == "" {
			title = "AI 生成公告"
		}
		if content == "" {
			content = strings.TrimSpace(msg)
		}
		return aiDecision{Reply: "将创建一条公告，请确认。", Action: &aiAction{Name: "create_star_notice", Args: map[string]any{"title": title, "content": content}}, RequiresConfirm: true}, true
	}
	if strings.Contains(msg, "今天异常操作") || strings.Contains(msg, "今日异常操作") || strings.Contains(msg, "异常操作") {
		return aiDecision{Reply: "今天异常操作如下。", Action: &aiAction{Name: "query_today_errors", Args: map[string]any{}}}, true
	}
	if strings.Contains(msg, "待审核动态") && strings.Contains(msg, "分组") {
		return aiDecision{Reply: "待审核动态按 UID 分组如下。", Action: &aiAction{Name: "group_pending_dynamics_by_uid", Args: map[string]any{}}}, true
	}
	if strings.Contains(msg, "自动审批") || strings.Contains(msg, "自动审核") || strings.Contains(msg, "AI 审动态") || strings.Contains(msg, "AI审动态") || (strings.Contains(msg, "审") && strings.Contains(msg, "动态") && (strings.Contains(msg, "全部") || strings.Contains(msg, "自动") || strings.Contains(msg, "批量") || strings.Contains(msg, "一键"))) {
		return aiDecision{Reply: "将用 AI 自动审批最多 50 条待审动态：违规直接隐藏、合规直接通过。请确认。", Action: &aiAction{Name: "auto_review_dynamics", Args: map[string]any{"limit": 50}}, RequiresConfirm: true}, true
	}
	if strings.Contains(msg, "运营日报") || strings.Contains(msg, "日报") {
		return aiDecision{Reply: "今日运营日报如下。", Action: &aiAction{Name: "operation_report", Args: map[string]any{}}}, true
	}
	if strings.Contains(msg, "清除背景") || strings.Contains(msg, "取消背景") {
		return aiDecision{Reply: "已清除当前背景。", Action: &aiAction{Name: "clear_wallpaper", Args: map[string]any{}}}, true
	}
	if strings.Contains(msg, "数据维护") || strings.Contains(msg, "备份页面") {
		return aiDecision{Reply: "已跳转到数据维护页面。", Action: &aiAction{Name: "navigate", Args: map[string]any{"view": "maintenance"}}}, true
	}
	switch {
	case strings.Contains(msg, "下载壁纸") || strings.Contains(msg, "保存壁纸"):
		return aiDecision{Reply: "已开始下载当前壁纸。", Action: &aiAction{Name: "download_wallpaper", Args: map[string]any{}}}, true
	case strings.Contains(msg, "上传壁纸") || strings.Contains(msg, "自定义壁纸") || strings.Contains(msg, "上传背景"):
		return aiDecision{Reply: "已打开图片选择器。", Action: &aiAction{Name: "upload_wallpaper", Args: map[string]any{}}}, true
	case strings.Contains(msg, "预览壁纸") || strings.Contains(msg, "退出预览") || strings.Contains(msg, "全屏壁纸") || strings.Contains(msg, "沉浸"):
		return aiDecision{Reply: "已切换壁纸预览。", Action: &aiAction{Name: "toggle_bg_preview", Args: map[string]any{}}}, true
	case strings.Contains(msg, "刷新") || strings.Contains(msg, "重新加载"):
		return aiDecision{Reply: "已刷新当前页面。", Action: &aiAction{Name: "refresh_view", Args: map[string]any{}}}, true
	case strings.Contains(msg, "退出登录") || strings.Contains(msg, "登出") || strings.Contains(msg, "注销"):
		return aiDecision{Reply: "确认退出登录吗？", Action: &aiAction{Name: "logout", Args: map[string]any{}}, RequiresConfirm: true}, true
	case strings.Contains(msg, "概览") || strings.Contains(msg, "今日新增") || strings.Contains(msg, "今天注册") || strings.Contains(msg, "统计数据") || strings.Contains(msg, "数据汇总"):
		return aiDecision{Reply: "概览数据如下。", Action: &aiAction{Name: "query_summary", Args: map[string]any{}}}, true
	case strings.Contains(msg, "在线用户") || strings.Contains(msg, "谁在线"):
		return aiDecision{Reply: "当前在线用户如下。", Action: &aiAction{Name: "query_online_users", Args: map[string]any{}}}, true
	case strings.Contains(msg, "待审核动态") || strings.Contains(msg, "待审动态"):
		return aiDecision{Reply: "待审核动态如下。", Action: &aiAction{Name: "query_pending_dynamics", Args: map[string]any{}}}, true
	case strings.Contains(msg, "管理员") && (strings.Contains(msg, "查询") || strings.Contains(msg, "列表") || strings.Contains(msg, "有哪些")):
		return aiDecision{Reply: "管理员账号如下。", Action: &aiAction{Name: "query_admins", Args: map[string]any{}}}, true
	case strings.Contains(msg, "最近用户") || strings.Contains(msg, "新用户") || strings.Contains(msg, "最近注册"):
		return aiDecision{Reply: "最近用户如下。", Action: &aiAction{Name: "query_recent_users", Args: map[string]any{"limit": 20}}}, true
	case strings.Contains(msg, "好友申请") || strings.Contains(msg, "申请列表"):
		return aiDecision{Reply: "好友申请如下。", Action: &aiAction{Name: "query_friend_applies", Args: map[string]any{}}}, true
	case strings.Contains(msg, "好友关系") || strings.Contains(msg, "好友列表"):
		return aiDecision{Reply: "好友关系如下。", Action: &aiAction{Name: "query_friends", Args: map[string]any{}}}, true
	case strings.Contains(msg, "通知") && (strings.Contains(msg, "查询") || strings.Contains(msg, "列表") || strings.Contains(msg, "投递")):
		return aiDecision{Reply: "通知投递记录如下。", Action: &aiAction{Name: "query_notices", Args: map[string]any{}}}, true
	case strings.Contains(msg, "公告"):
		return aiDecision{Reply: "公告列表如下。", Action: &aiAction{Name: "query_star_notices", Args: map[string]any{}}}, true
	case strings.HasPrefix(msg, "搜索") || strings.HasPrefix(msg, "全局搜索") || strings.HasPrefix(msg, "聚合搜索"):
		q := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(msg, "聚合搜索"), "全局搜索"), "搜索"))
		return aiDecision{Reply: "聚合搜索结果如下。", Action: &aiAction{Name: "search_all", Args: map[string]any{"q": q}}}, true
	case strings.HasPrefix(msg, "打开") || strings.HasPrefix(msg, "跳到") || strings.HasPrefix(msg, "跳转"):
		if v := matchView(msg); v != "" {
			return aiDecision{Reply: "已跳转。", Action: &aiAction{Name: "navigate", Args: map[string]any{"view": v}}}, true
		}
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

func parseNoticeLikeText(s string) (string, string) {
	s = strings.TrimSpace(s)
	title := ""
	content := ""
	if idx := strings.Index(s, "标题"); idx >= 0 {
		rest := strings.TrimSpace(s[idx+len("标题"):])
		rest = strings.TrimLeft(rest, " ：:")
		if cidx := strings.Index(rest, "内容"); cidx >= 0 {
			title = strings.TrimSpace(strings.TrimRight(rest[:cidx], " ：:，,"))
			content = strings.TrimSpace(strings.TrimLeft(rest[cidx+len("内容"):], " ：:"))
		} else {
			title = rest
		}
	}
	if content == "" {
		for _, sep := range []string{"：", ":"} {
			if idx := strings.LastIndex(s, sep); idx >= 0 && idx+len(sep) < len(s) {
				content = strings.TrimSpace(s[idx+len(sep):])
				break
			}
		}
	}
	return title, content
}

func isClientOnlyAIAction(name string) bool {
	switch name {
	case "switch_wallpaper", "set_theme",
		"download_wallpaper", "upload_wallpaper", "toggle_bg_preview",
		"set_wallpaper_url", "clear_wallpaper",
		"navigate", "refresh_view", "logout":
		return true
	default:
		return false
	}
}

func isHighRiskAIAction(name string) bool {
	switch name {
	case "delete_user", "delete_dynamic", "update_dynamic_status", "send_notice",
		"create_star_notice", "logout", "batch_hide_dynamics_by_keyword", "update_user_role",
		"auto_review_dynamics":
		return true
	default:
		return false
	}
}

func matchView(msg string) string {
	switch {
	case strings.Contains(msg, "概览") || strings.Contains(msg, "首页") || strings.Contains(msg, "仪表"):
		return "dashboard"
	case strings.Contains(msg, "用户"):
		return "users"
	case strings.Contains(msg, "动态"):
		return "dynamics"
	case strings.Contains(msg, "好友关系") || strings.Contains(msg, "好友列表"):
		return "friends"
	case strings.Contains(msg, "好友申请") || strings.Contains(msg, "申请"):
		return "applies"
	case strings.Contains(msg, "公告") || strings.Contains(msg, "star"):
		return "star"
	case strings.Contains(msg, "通知"):
		return "notices"
	case strings.Contains(msg, "ai") || strings.Contains(msg, "AI") || strings.Contains(msg, "助手"):
		return "ai"
	}
	return ""
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
	case "query_notices":
		q := strings.TrimSpace(strArg(action.Args, "q"))
		conditions := []string{}
		args := []any{}
		if q != "" {
			kw := "%" + q + "%"
			conditions = append(conditions, "(n.title LIKE ? OR n.content LIKE ? OR CAST(n.target_uid AS CHAR) LIKE ? OR u.name LIKE ?)")
			args = append(args, kw, kw, kw, kw)
		}
		if level := strings.TrimSpace(strArg(action.Args, "level")); level != "" {
			conditions = append(conditions, "n.level = ?")
			args = append(args, level)
		}
		if _, ok := action.Args["delivered"]; ok {
			conditions = append(conditions, "n.delivered = ?")
			args = append(args, intArg(action.Args, "delivered"))
		}
		where := ""
		if len(conditions) > 0 {
			where = "WHERE " + strings.Join(conditions, " AND ")
		}
		return a.queryMaps(`SELECT n.id, n.target_uid, u.name AS target_name, n.title, n.content, n.level, n.delivered, n.create_time
			FROM admin_notice n
			LEFT JOIN `+"`user`"+` u ON n.target_uid = u.uid
			`+where+` ORDER BY n.id DESC LIMIT 20`, args...)
	case "query_friends":
		q := strings.TrimSpace(strArg(action.Args, "q"))
		args := []any{}
		where := ""
		if q != "" {
			kw := "%" + q + "%"
			where = "WHERE CAST(f.self_id AS CHAR) LIKE ? OR CAST(f.friend_id AS CHAR) LIKE ? OR su.name LIKE ? OR fu.name LIKE ? OR su.nick LIKE ? OR fu.nick LIKE ?"
			args = append(args, kw, kw, kw, kw, kw, kw)
		}
		return a.queryMaps(`SELECT f.self_id, su.name AS self_name, su.nick AS self_nick,
			f.friend_id, fu.name AS friend_name, fu.nick AS friend_nick, f.back
			FROM friend f
			LEFT JOIN `+"`user`"+` su ON f.self_id = su.uid
			LEFT JOIN `+"`user`"+` fu ON f.friend_id = fu.uid `+where+`
			ORDER BY f.self_id ASC, f.friend_id ASC LIMIT 30`, args...)
	case "query_online_users":
		return a.queryMaps("SELECT uid, name, email, nick, role, status FROM `user` WHERE status = 1 ORDER BY uid ASC LIMIT 50")
	case "query_recent_users":
		limit := intArg(action.Args, "limit")
		if limit <= 0 || limit > 50 {
			limit = 20
		}
		return a.queryMaps("SELECT uid, name, email, nick, role, status FROM `user` ORDER BY uid DESC LIMIT ?", limit)
	case "query_pending_dynamics":
		return a.queryMaps(`SELECT d.id, d.uid, u.name, d.content, d.like_count, d.status, d.create_time
			FROM `+"`dynamic`"+` d LEFT JOIN `+"`user`"+` u ON d.uid = u.uid
			WHERE d.status = 1 ORDER BY d.id DESC LIMIT 30`)
	case "query_admins":
		return a.queryMaps("SELECT uid, name, email, nick, role, status FROM `user` WHERE role > 0 ORDER BY role DESC, uid ASC LIMIT 50")
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
	case "query_summary":
		rows, err := a.queryMaps(`SELECT
			(SELECT COUNT(*) FROM ` + "`user`" + `) AS users,
			(SELECT COUNT(*) FROM ` + "`dynamic`" + `) AS dynamics,
			(SELECT COUNT(*) FROM ` + "`dynamic`" + ` WHERE DATE(create_time) = CURDATE()) AS today_dynamics,
			(SELECT COUNT(*) FROM ` + "`dynamic`" + ` WHERE status = 1) AS pending_dynamics,
			(SELECT COUNT(*) FROM admin_operation_log WHERE module = 'user' AND action = 'create' AND DATE(create_time) = CURDATE()) AS today_users,
			(SELECT COUNT(*) FROM admin_operation_log WHERE DATE(create_time) = CURDATE()) AS today_operations,
			(SELECT COUNT(*) FROM friend_apply WHERE status = 0) AS pending_applies,
			(SELECT COUNT(*) FROM admin_notice) AS notices`)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return map[string]any{}, nil
		}
		return rows[0], nil
	case "query_friend_applies":
		q := strings.TrimSpace(strArg(action.Args, "q"))
		conditions := []string{}
		args := []any{}
		if q != "" {
			kw := "%" + q + "%"
			conditions = append(conditions, "(CAST(fa.from_uid AS CHAR) LIKE ? OR CAST(fa.to_uid AS CHAR) LIKE ? OR fa.back_name LIKE ? OR uf.name LIKE ? OR ut.name LIKE ?)")
			args = append(args, kw, kw, kw, kw, kw)
		}
		if _, ok := action.Args["status"]; ok {
			s := intArg(action.Args, "status")
			conditions = append(conditions, "fa.status = ?")
			args = append(args, s)
		}
		where := ""
		if len(conditions) > 0 {
			where = "WHERE " + strings.Join(conditions, " AND ")
		}
		sqlStr := `SELECT fa.id, fa.from_uid, uf.name AS from_name, fa.to_uid, ut.name AS to_name,
			fa.back_name, fa.status
			FROM friend_apply fa
			LEFT JOIN ` + "`user`" + ` uf ON fa.from_uid = uf.uid
			LEFT JOIN ` + "`user`" + ` ut ON fa.to_uid = ut.uid
			` + where + ` ORDER BY fa.id DESC LIMIT 20`
		return a.queryMaps(sqlStr, args...)
	case "query_star_notices":
		q := strings.TrimSpace(strArg(action.Args, "q"))
		if q == "" {
			return a.queryMaps("SELECT title, author, content FROM StarNotice ORDER BY title ASC LIMIT 20")
		}
		kw := "%" + q + "%"
		return a.queryMaps("SELECT title, author, content FROM StarNotice WHERE title LIKE ? OR author LIKE ? OR content LIKE ? ORDER BY title ASC LIMIT 20", kw, kw, kw)
	case "search_all":
		q := strings.TrimSpace(strArg(action.Args, "q"))
		if q == "" {
			return nil, errors.New("请提供搜索关键词")
		}
		kw := "%" + q + "%"
		users, err := a.queryMaps("SELECT uid, name, email, nick, role, status FROM `user` WHERE name LIKE ? OR email LIKE ? OR nick LIKE ? OR CAST(uid AS CHAR) LIKE ? ORDER BY uid ASC LIMIT 10", kw, kw, kw, kw)
		if err != nil {
			return nil, err
		}
		dynamics, err := a.queryMaps("SELECT d.id, d.uid, u.name, d.content, d.status, d.create_time FROM `dynamic` d LEFT JOIN `user` u ON d.uid = u.uid WHERE d.content LIKE ? OR u.name LIKE ? OR CAST(d.uid AS CHAR) LIKE ? ORDER BY d.id DESC LIMIT 10", kw, kw, kw)
		if err != nil {
			return nil, err
		}
		logs, err := a.queryMaps("SELECT id, module, action, summary, operator, create_time FROM admin_operation_log WHERE summary LIKE ? OR operator LIKE ? OR module LIKE ? OR action LIKE ? ORDER BY id DESC LIMIT 10", kw, kw, kw, kw)
		if err != nil {
			return nil, err
		}
		notices, err := a.queryMaps("SELECT id, target_uid, title, level, delivered, create_time FROM admin_notice WHERE title LIKE ? OR content LIKE ? OR CAST(target_uid AS CHAR) LIKE ? ORDER BY id DESC LIMIT 10", kw, kw, kw)
		if err != nil {
			return nil, err
		}
		starNotices, err := a.queryMaps("SELECT title, author, content FROM StarNotice WHERE title LIKE ? OR author LIKE ? OR content LIKE ? ORDER BY title ASC LIMIT 10", kw, kw, kw)
		if err != nil {
			return nil, err
		}
		return map[string]any{"keyword": q, "users": users, "dynamics": dynamics, "logs": logs, "notices": notices, "star_notices": starNotices}, nil
	case "batch_hide_dynamics_by_keyword":
		q := strings.TrimSpace(strArg(action.Args, "q"))
		if q == "" {
			return nil, errors.New("批量隐藏必须提供关键词")
		}
		kw := "%" + q + "%"
		preview, err := a.queryMaps("SELECT id, uid, content, status FROM `dynamic` WHERE content LIKE ? AND status <> 2 ORDER BY id DESC LIMIT 50", kw)
		if err != nil {
			return nil, err
		}
		res, err := a.db.Exec("UPDATE `dynamic` SET status = 2 WHERE content LIKE ? AND status <> 2", kw)
		if err != nil {
			return nil, err
		}
		n := int(affected(res))
		a.logOperation(r, operator, "ai", "batch_hide_dynamics", "dynamic", "", nil, fmt.Sprintf("AI 批量隐藏含 \"%s\" 的动态 %d 条", q, n), action.Args)
		return map[string]any{"ok": true, "affected": n, "preview": preview}, nil
	case "create_star_notice":
		title := strings.TrimSpace(strArg(action.Args, "title"))
		content := strings.TrimSpace(strArg(action.Args, "content"))
		author := strings.TrimSpace(strArg(action.Args, "author"))
		if author == "" {
			author = operator
		}
		if title == "" || content == "" {
			return nil, errors.New("公告标题和内容不能为空")
		}
		res, err := a.db.Exec("INSERT INTO StarNotice (title, author, content) VALUES (?, ?, ?)", title, author, content)
		if err != nil {
			return nil, err
		}
		a.logOperation(r, operator, "ai", "create_star_notice", "star_notice", title, nil, fmt.Sprintf("AI 生成公告 %s", title), action.Args)
		return map[string]any{"ok": affected(res) > 0, "title": title, "author": author, "content": content}, nil
	case "query_today_errors":
		rows, err := a.queryMaps(`SELECT id, module, action, summary, operator, ip, create_time
			FROM admin_operation_log
			WHERE DATE(create_time) = CURDATE()
			  AND (action LIKE '%failed%' OR action LIKE '%denied%' OR action = 'delete'
			       OR action LIKE '%error%' OR summary LIKE '%失败%' OR summary LIKE '%错误%')
			ORDER BY id DESC LIMIT 50`)
		if err != nil {
			return nil, err
		}
		return map[string]any{"date": time.Now().Format("2006-01-02"), "errors": rows, "total": len(rows)}, nil
	case "group_pending_dynamics_by_uid":
		rows, err := a.queryMaps(`SELECT d.uid, u.name, COUNT(*) AS pending_count,
				GROUP_CONCAT(d.id ORDER BY d.id DESC SEPARATOR ',') AS dynamic_ids
			FROM ` + "`dynamic`" + ` d LEFT JOIN ` + "`user`" + ` u ON d.uid = u.uid
			WHERE d.status = 1
			GROUP BY d.uid, u.name
			ORDER BY pending_count DESC, d.uid ASC LIMIT 50`)
		if err != nil {
			return nil, err
		}
		var total int
		for _, row := range rows {
			if c, ok := row["pending_count"]; ok {
				switch v := c.(type) {
				case int64:
					total += int(v)
				case float64:
					total += int(v)
				}
			}
		}
		return map[string]any{"groups": rows, "users_count": len(rows), "total_dynamics": total}, nil
	case "operation_report":
		today := time.Now().Format("2006-01-02")
		summaryRows, err := a.queryMaps(`SELECT
			(SELECT COUNT(*) FROM ` + "`user`" + `) AS total_users,
			(SELECT COUNT(*) FROM ` + "`user`" + ` WHERE status = 1) AS online_users,
			(SELECT COUNT(*) FROM ` + "`dynamic`" + ` WHERE DATE(create_time) = CURDATE()) AS today_dynamics,
			(SELECT COUNT(*) FROM ` + "`dynamic`" + ` WHERE status = 1) AS pending_dynamics,
			(SELECT COUNT(*) FROM friend_apply WHERE status = 0) AS pending_applies,
			(SELECT COUNT(*) FROM admin_notice WHERE DATE(create_time) = CURDATE()) AS today_notices,
			(SELECT COUNT(*) FROM admin_operation_log WHERE DATE(create_time) = CURDATE()) AS today_operations,
			(SELECT COUNT(*) FROM ai_chat_message WHERE DATE(create_time) = CURDATE() AND role = 'user') AS today_ai_chats`)
		if err != nil {
			return nil, err
		}
		var summary map[string]any
		if len(summaryRows) > 0 {
			summary = summaryRows[0]
		}
		topOps, _ := a.queryMaps(`SELECT operator, COUNT(*) AS ops
			FROM admin_operation_log
			WHERE DATE(create_time) = CURDATE()
			GROUP BY operator ORDER BY ops DESC LIMIT 5`)
		topModules, _ := a.queryMaps(`SELECT module, COUNT(*) AS ops
			FROM admin_operation_log
			WHERE DATE(create_time) = CURDATE()
			GROUP BY module ORDER BY ops DESC LIMIT 5`)
		return map[string]any{
			"date":          today,
			"summary":       summary,
			"top_operators": topOps,
			"top_modules":   topModules,
		}, nil
	case "auto_review_dynamics":
		return a.runAutoReviewDynamics(r, operator, intArg(action.Args, "limit"))
	case "update_user_role":
		uid := intArg(action.Args, "uid")
		role := intArg(action.Args, "role")
		if uid <= 0 || role < 0 {
			return nil, errors.New("UID 或角色参数不正确")
		}
		operatorUID, operatorRole, targetRole, err := a.getOperatorAndTarget(operator, uid)
		if err != nil {
			return nil, errors.New("用户不存在")
		}
		if operatorUID == uid {
			return nil, errors.New("不能修改自己的角色")
		}
		if targetRole >= operatorRole {
			return nil, errors.New("不能修改权限高于或等于自己的账号")
		}
		if role >= operatorRole {
			return nil, errors.New("不能将角色提升到等于或高于自己")
		}
		res, err := a.db.Exec("UPDATE `user` SET role = ? WHERE uid = ?", role, uid)
		if err != nil {
			return nil, err
		}
		a.logOperation(r, operator, "ai", "update_user_role", "user", strconv.Itoa(uid), &uid, fmt.Sprintf("AI 修改 UID %d 角色 %d -> %d", uid, targetRole, role), action.Args)
		return map[string]any{"ok": affected(res) > 0, "uid": uid, "role": role}, nil
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
	case float32:
		return int(v)
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
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

func (a *app) aiOptimizeText(w http.ResponseWriter, r *http.Request, operator string) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	var p struct {
		Text string `json:"text"`
	}
	if !decodeJSON(w, r, &p) {
		return
	}
	p.Text = strings.TrimSpace(p.Text)
	if p.Text == "" {
		writeErr(w, http.StatusBadRequest, "内容不能为空")
		return
	}
	if a.cfg.AIAPIKey == "" {
		writeErr(w, http.StatusBadRequest, "AI API Key 未配置")
		return
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

	messages := []openAIMessage{
		{Role: "system", Content: "你是一位专业的文案编辑。请优化用户提供的公告/通知内容，使其更加专业、简洁、有吸引力。保持原意不变，适当润色措辞，修正语病。只返回优化后的正文内容，不要加任何前缀说明或 markdown 格式。需要把内容改的更像公告，需要保持原有的内容，不能篡改含义原有的。"},
		{Role: "user", Content: p.Text},
	}
	reqBody := openAIChatRequest{Model: model, Messages: messages, Temperature: 0.3}
	b, _ := json.Marshal(reqBody)

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatURL, bytes.NewReader(b))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.AIAPIKey)

	resp, err := (&http.Client{Timeout: 25 * time.Second}).Do(req)
	if err != nil {
		writeErr(w, 502, "AI 请求失败: "+err.Error())
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		writeErr(w, 502, fmt.Sprintf("AI 返回 %d: %s", resp.StatusCode, string(body)))
		return
	}
	var aiResp openAIChatResponse
	if err := json.Unmarshal(body, &aiResp); err != nil || len(aiResp.Choices) == 0 {
		writeErr(w, 502, "AI 响应解析失败")
		return
	}
	result := strings.TrimSpace(aiResp.Choices[0].Message.Content)
	writeJSON(w, map[string]any{"result": result})
}
