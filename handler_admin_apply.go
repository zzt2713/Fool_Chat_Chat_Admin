package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type adminApplyPayload struct {
	Account string `json:"account"`
	Email   string `json:"email"`
}

type adminApplyReviewPayload struct {
	ID       int64  `json:"id"`
	Approve  bool   `json:"approve"`
	Note     string `json:"note"`
}

func (a *app) adminApplyStatus(w http.ResponseWriter, r *http.Request) {
	account := strings.TrimSpace(r.URL.Query().Get("account"))
	if account == "" {
		writeErr(w, http.StatusBadRequest, "缺少 account 参数")
		return
	}
	var status string
	err := a.db.QueryRow("SELECT status FROM admin_application WHERE account = ? ORDER BY create_time DESC LIMIT 1", account).Scan(&status)
	if err == sql.ErrNoRows {
		writeJSON(w, map[string]any{"status": ""})
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"status": status})
}

func (a *app) adminApplySubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	var p adminApplyPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	p.Account = strings.TrimSpace(p.Account)
	p.Email = strings.TrimSpace(p.Email)
	if p.Account == "" || p.Email == "" {
		writeErr(w, http.StatusBadRequest, "账号和邮箱不能为空")
		return
	}
	if !strings.Contains(p.Email, "@") {
		writeErr(w, http.StatusBadRequest, "邮箱格式不正确")
		return
	}

	ip := r.RemoteAddr
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		ip = strings.Split(h, ",")[0]
	}

	// Rate limit: 1 per 5 minutes per IP
	var recent int
	_ = a.db.QueryRow("SELECT COUNT(*) FROM admin_application WHERE ip = ? AND create_time > DATE_SUB(NOW(), INTERVAL 5 MINUTE)", ip).Scan(&recent)
	if recent > 0 {
		writeErr(w, http.StatusTooManyRequests, "提交过于频繁，请5分钟后再试")
		return
	}

	// Check pending application
	var pending int
	_ = a.db.QueryRow("SELECT COUNT(*) FROM admin_application WHERE account = ? AND status = 'pending'", p.Account).Scan(&pending)
	if pending > 0 {
		writeErr(w, http.StatusConflict, "你已有一条待审核的申请，请耐心等待")
		return
	}

	// Check if account exists and is not already admin
	var role int
	err := a.db.QueryRow("SELECT role FROM `user` WHERE name = ? LIMIT 1", p.Account).Scan(&role)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusBadRequest, "账号不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if role == 1 || role == 2 {
		writeErr(w, http.StatusBadRequest, "该账号已是管理员")
		return
	}

	res, err := a.db.Exec("INSERT INTO admin_application (account, email, ip) VALUES (?, ?, ?)", p.Account, p.Email, ip)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	a.logOperation(r, "system", "admin_apply", "submit", "admin_application", fmt.Sprint(id), nil,
		fmt.Sprintf("管理员申请: %s (%s)", p.Account, p.Email), nil)

	writeJSON(w, map[string]any{"ok": true, "id": id})
}

func (a *app) adminApplyList(w http.ResponseWriter, r *http.Request, operator string) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	if !a.isSuperAdmin(operator) {
		writeErr(w, http.StatusForbidden, "无权限")
		return
	}
	status := r.URL.Query().Get("status")
	limit, offset := pageLimit(r.URL.Query(), 20, 100)

	where := ""
	args := []any{}
	if status != "" {
		where = "WHERE status = ?"
		args = append(args, status)
	}
	args = append(args, limit, offset)

	rows, err := a.queryMaps("SELECT id, account, email, status, review_note, reviewed_by, review_time, ip, create_time FROM admin_application "+where+" ORDER BY create_time DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}

	var total int
	countArgs := []any{}
	countWhere := ""
	if status != "" {
		countWhere = "WHERE status = ?"
		countArgs = append(countArgs, status)
	}
	_ = a.db.QueryRow("SELECT COUNT(*) FROM admin_application "+countWhere, countArgs...).Scan(&total)

	writeJSON(w, map[string]any{"items": rows, "total": total})
}

func (a *app) adminApplyReview(w http.ResponseWriter, r *http.Request, operator string) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	if !a.isSuperAdmin(operator) {
		writeErr(w, http.StatusForbidden, "无权限")
		return
	}
	var p adminApplyReviewPayload
	if !decodeJSON(w, r, &p) {
		return
	}

	var account, email, status string
	err := a.db.QueryRow("SELECT account, email, status FROM admin_application WHERE id = ?", p.ID).Scan(&account, &email, &status)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "申请不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if status != "pending" {
		writeErr(w, http.StatusBadRequest, "该申请已处理")
		return
	}

	now := time.Now()
	if p.Approve {
		// Set user role to 1
		res, err := a.db.Exec("UPDATE `user` SET role = 1 WHERE name = ?", account)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeErr(w, http.StatusBadRequest, "账号不存在")
			return
		}
		a.invalidateSuperAdminCache(account)
		_, _ = a.db.Exec("UPDATE admin_application SET status = 'approved', review_note = ?, reviewed_by = ?, review_time = ? WHERE id = ?",
			p.Note, operator, now, p.ID)

		a.logOperation(r, operator, "admin_apply", "approve", "admin_application", fmt.Sprint(p.ID), nil,
			fmt.Sprintf("批准管理员申请: %s", account), map[string]any{"note": p.Note})

		// Send approval email
		go a.sendApprovalEmail(email, account)
	} else {
		note := strings.TrimSpace(p.Note)
		if note == "" {
			note = "暂不满足管理员条件"
		}
		_, _ = a.db.Exec("UPDATE admin_application SET status = 'rejected', review_note = ?, reviewed_by = ?, review_time = ? WHERE id = ?",
			note, operator, now, p.ID)

		a.logOperation(r, operator, "admin_apply", "reject", "admin_application", fmt.Sprint(p.ID), nil,
			fmt.Sprintf("拒绝管理员申请: %s", account), map[string]any{"note": note})

		// AI 生成拒绝邮件内容，发送邮件
		go func() {
			aiReply := a.generateAIReply(context.Background(), account, email, note)
			if aiReply == "" {
				aiReply = note
			}
			a.sendRejectionEmail(email, account, aiReply)
		}()
	}

	writeJSON(w, map[string]any{"ok": true})
}

func (a *app) adminApplyAIRejectAll(w http.ResponseWriter, r *http.Request, operator string) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	if !a.isSuperAdmin(operator) {
		writeErr(w, http.StatusForbidden, "无权限")
		return
	}

	rows, err := a.db.Query("SELECT id, account, email FROM admin_application WHERE status = 'pending'")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type pendingApp struct {
		ID      int64
		Account string
		Email   string
	}
	var apps []pendingApp
	for rows.Next() {
		var item pendingApp
		if err := rows.Scan(&item.ID, &item.Account, &item.Email); err != nil {
			continue
		}
		apps = append(apps, item)
	}

	if len(apps) == 0 {
		writeJSON(w, map[string]any{"count": 0})
		return
	}

	now := time.Now()
	for _, app := range apps {
		_, _ = a.db.Exec("UPDATE admin_application SET status = 'rejected', review_note = 'AI 一键拒绝', reviewed_by = ?, review_time = ? WHERE id = ?",
			operator, now, app.ID)
		a.logOperation(r, operator, "admin_apply", "reject", "admin_application", fmt.Sprint(app.ID), nil,
			fmt.Sprintf("AI 一键拒绝管理员申请: %s", app.Account), nil)
		go func(account, email string) {
			aiReply := a.generateAIReply(context.Background(), account, email, "暂不满足管理员条件")
			if aiReply == "" {
				aiReply = "暂不满足管理员条件"
			}
			a.sendRejectionEmail(email, account, aiReply)
		}(app.Account, app.Email)
	}

	writeJSON(w, map[string]any{"count": len(apps)})
}

func (a *app) sendApprovalEmail(to, account string) {
	subject := "Fool Chat 管理员申请已通过"
	body := fmt.Sprintf(`<div style="font-family:system-ui,sans-serif;max-width:600px;margin:0 auto;padding:20px">
<h2 style="color:#16a34a">申请已通过</h2>
<p>亲爱的 %s：</p>
<p>恭喜你！你的管理员申请已通过审核。</p>
<p>你现在可以使用管理员账号登录后台管理系统。</p>
<p style="color:#666;margin-top:30px">Fool Chat 团队</p>
</div>`, account)
	if err := a.sendSMTP(to, subject, body); err != nil {
		log.Printf("[WARN] send approval email to %s failed: %v", to, err)
	}
}

func (a *app) sendRejectionEmail(to, account, reason string) {
	subject := "Fool Chat 管理员申请结果"
	body := fmt.Sprintf(`<div style="font-family:system-ui,sans-serif;max-width:600px;margin:0 auto;padding:20px">
<h2 style="color:#dc2626">申请结果</h2>
<p>%s</p>
<p style="color:#666;margin-top:30px">Fool Chat 团队</p>
</div>`, strings.ReplaceAll(reason, "\n", "<br>"))
	if err := a.sendSMTP(to, subject, body); err != nil {
		log.Printf("[WARN] send rejection email to %s failed: %v", to, err)
	}
}

func (a *app) isSuperAdmin(name string) bool {
	a.saCacheMu.RLock()
	if v, ok := a.superAdminCache[name]; ok {
		a.saCacheMu.RUnlock()
		return v
	}
	a.saCacheMu.RUnlock()

	var role string
	err := a.db.QueryRow("SELECT role FROM `user` WHERE name = ? LIMIT 1", name).Scan(&role)
	isSA := err == nil && role == "2"

	a.saCacheMu.Lock()
	a.superAdminCache[name] = isSA
	a.saCacheMu.Unlock()
	return isSA
}

func (a *app) invalidateSuperAdminCache(name string) {
	a.saCacheMu.Lock()
	delete(a.superAdminCache, name)
	a.saCacheMu.Unlock()
}
