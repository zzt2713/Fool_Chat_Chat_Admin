package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type emailDraftPayload struct {
	ID          int64  `json:"id"`
	Subject     string `json:"subject"`
	Content     string `json:"content"`
	TargetType  string `json:"target_type"`
	TargetEmail string `json:"target_email"`
}

func (a *app) emailDraftList(w http.ResponseWriter, r *http.Request, operator string) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
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

	rows, err := a.queryMaps("SELECT id, subject, content, target_type, target_email, status, error_msg, operator, send_time, create_time FROM email_draft "+where+" ORDER BY create_time DESC LIMIT ? OFFSET ?", args...)
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
	_ = a.db.QueryRow("SELECT COUNT(*) FROM email_draft "+countWhere, countArgs...).Scan(&total)

	writeJSON(w, map[string]any{"items": rows, "total": total})
}

func (a *app) emailDraftSave(w http.ResponseWriter, r *http.Request, operator string) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	var p emailDraftPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	p.Subject = strings.TrimSpace(p.Subject)
	p.Content = strings.TrimSpace(p.Content)
	p.TargetType = strings.TrimSpace(p.TargetType)
	if p.TargetType == "" {
		p.TargetType = "all"
	}
	if p.TargetType != "all" && p.TargetType != "single" {
		writeErr(w, http.StatusBadRequest, "target_type 必须为 all 或 single")
		return
	}
	if p.TargetType == "single" {
		p.TargetEmail = strings.TrimSpace(p.TargetEmail)
		if p.TargetEmail == "" {
			writeErr(w, http.StatusBadRequest, "选择单个用户时邮箱不能为空")
			return
		}
	}
	if p.Subject == "" {
		writeErr(w, http.StatusBadRequest, "标题不能为空")
		return
	}

	now := time.Now()
	if p.ID > 0 {
		_, err := a.db.Exec("UPDATE email_draft SET subject=?, content=?, target_type=?, target_email=?, status='draft' WHERE id=? AND (status='draft' OR status='failed')",
			p.Subject, p.Content, p.TargetType, p.TargetEmail, p.ID)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		a.logOperation(r, operator, "email_draft", "update", "email_draft", fmt.Sprint(p.ID), nil,
			fmt.Sprintf("更新邮件草稿: %s", p.Subject), nil)
		writeJSON(w, map[string]any{"ok": true, "id": p.ID})
	} else {
		res, err := a.db.Exec("INSERT INTO email_draft (subject, content, target_type, target_email, operator, create_time) VALUES (?, ?, ?, ?, ?, ?)",
			p.Subject, p.Content, p.TargetType, p.TargetEmail, operator, now)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		id, _ := res.LastInsertId()
		a.logOperation(r, operator, "email_draft", "create", "email_draft", fmt.Sprint(id), nil,
			fmt.Sprintf("创建邮件草稿: %s", p.Subject), nil)
		writeJSON(w, map[string]any{"ok": true, "id": id})
	}
}

func (a *app) emailDraftSend(w http.ResponseWriter, r *http.Request, operator string) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	var p struct {
		ID int64 `json:"id"`
	}
	if !decodeJSON(w, r, &p) {
		return
	}

	var subject, content, targetType, targetEmail, status string
	err := a.db.QueryRow("SELECT subject, content, target_type, target_email, status FROM email_draft WHERE id = ?", p.ID).Scan(&subject, &content, &targetType, &targetEmail, &status)
	if err != nil {
		writeErr(w, http.StatusNotFound, "邮件不存在")
		return
	}
	if status == "sent" || status == "sending" {
		writeErr(w, http.StatusBadRequest, "该邮件已发送或正在发送中")
		return
	}
	if subject == "" {
		writeErr(w, http.StatusBadRequest, "标题不能为空")
		return
	}

	htmlBody := fmt.Sprintf(`<div style="font-family:system-ui,sans-serif;max-width:600px;margin:0 auto;padding:20px">
<h2 style="color:#1f2937">%s</h2>
<div style="color:#374151;line-height:1.7">%s</div>
<p style="color:#9ca3af;margin-top:30px;font-size:12px">Fool Chat 团队</p>
</div>`, subject, strings.ReplaceAll(content, "\n", "<br>"))

	_, _ = a.db.Exec("UPDATE email_draft SET status='sending', error_msg=NULL WHERE id=?", p.ID)

	a.logOperation(r, operator, "email_draft", "send", "email_draft", fmt.Sprint(p.ID), nil,
		fmt.Sprintf("发送邮件通知: %s (目标:%s)", subject, targetType), nil)

	go func() {
		now := time.Now()
		if targetType == "single" {
			if targetEmail == "" {
				_, _ = a.db.Exec("UPDATE email_draft SET status='failed', error_msg='目标邮箱为空', send_time=? WHERE id=?", now, p.ID)
				return
			}
			if err := a.sendSMTP(targetEmail, subject, htmlBody); err != nil {
				log.Printf("[WARN] email draft %d send to %s failed: %v", p.ID, targetEmail, err)
				_, _ = a.db.Exec("UPDATE email_draft SET status='failed', error_msg=?, send_time=? WHERE id=?", err.Error(), now, p.ID)
			} else {
				_, _ = a.db.Exec("UPDATE email_draft SET status='sent', send_time=? WHERE id=?", now, p.ID)
			}
		} else {
			rows, err := a.db.Query("SELECT email FROM `user` WHERE email IS NOT NULL AND email != ''")
			if err != nil {
				_, _ = a.db.Exec("UPDATE email_draft SET status='failed', error_msg=?, send_time=? WHERE id=?", err.Error(), now, p.ID)
				return
			}
			defer rows.Close()
			var failCount int
			var lastErr string
			for rows.Next() {
				var email string
				if err := rows.Scan(&email); err != nil {
					continue
				}
				if err := a.sendSMTP(email, subject, htmlBody); err != nil {
					failCount++
					lastErr = err.Error()
					log.Printf("[WARN] email draft %d send to %s failed: %v", p.ID, email, err)
				}
			}
			if failCount > 0 {
				_, _ = a.db.Exec("UPDATE email_draft SET status='failed', error_msg=?, send_time=? WHERE id=?",
					fmt.Sprintf("部分发送失败(%d封): %s", failCount, lastErr), now, p.ID)
			} else {
				_, _ = a.db.Exec("UPDATE email_draft SET status='sent', send_time=? WHERE id=?", now, p.ID)
			}
		}
	}()

	writeJSON(w, map[string]any{"ok": true})
}

func (a *app) emailDraftDelete(w http.ResponseWriter, r *http.Request, operator string) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	var p struct {
		ID int64 `json:"id"`
	}
	if !decodeJSON(w, r, &p) {
		return
	}

	var status string
	err := a.db.QueryRow("SELECT status FROM email_draft WHERE id = ?", p.ID).Scan(&status)
	if err != nil {
		writeErr(w, http.StatusNotFound, "邮件不存在")
		return
	}
	if status == "sent" || status == "sending" {
		writeErr(w, http.StatusBadRequest, "已发送或发送中的邮件不能删除")
		return
	}

	_, err = a.db.Exec("DELETE FROM email_draft WHERE id = ? AND (status = 'draft' OR status = 'failed')", p.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	a.logOperation(r, operator, "email_draft", "delete", "email_draft", fmt.Sprint(p.ID), nil,
		fmt.Sprintf("删除邮件草稿: id=%d", p.ID), nil)
	writeJSON(w, map[string]any{"ok": true})
}
