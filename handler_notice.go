package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type starPayload struct {
	Title   string `json:"title"`
	Author  string `json:"author"`
	Content string `json:"content"`
}

type noticePayload struct {
	TargetUID *int   `json:"target_uid"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Level     string `json:"level"`
	Delivered int    `json:"delivered"`
}

type bgPayload struct {
	URL string `json:"url"`
}

func (a *app) friends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	q := trim(r.URL.Query().Get("q"), 0)
	limit, offset := pageLimit(r.URL.Query(), 80, 200)
	args := []any{}
	where := ""
	if q != "" {
		kw := "%" + q + "%"
		where = "WHERE CAST(f.self_id AS CHAR) LIKE ? OR CAST(f.friend_id AS CHAR) LIKE ? OR su.name LIKE ? OR fu.name LIKE ? OR su.nick LIKE ? OR fu.nick LIKE ?"
		args = append(args, kw, kw, kw, kw, kw, kw)
	}
	args = append(args, limit, offset)
	rows, err := a.queryMaps(`SELECT f.self_id, su.name AS self_name, su.nick AS self_nick,
		f.friend_id, fu.name AS friend_name, fu.nick AS friend_nick, f.back
		FROM friend f
		LEFT JOIN `+"`user`"+` su ON f.self_id = su.uid
		LEFT JOIN `+"`user`"+` fu ON f.friend_id = fu.uid `+where+`
		ORDER BY f.self_id ASC, f.friend_id ASC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var total int
	countArgs := []any{}
	if q != "" {
		kw := "%" + q + "%"
		countArgs = append(countArgs, kw, kw, kw, kw, kw, kw)
	}
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM friend f
		LEFT JOIN `+"`user`"+` su ON f.self_id = su.uid
		LEFT JOIN `+"`user`"+` fu ON f.friend_id = fu.uid `+where, countArgs...).Scan(&total)
	writeJSON(w, map[string]any{"items": rows, "total": total})
}

func (a *app) friendByIDs(w http.ResponseWriter, r *http.Request, operator string) {
	if r.Method != http.MethodDelete {
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	ids := strings.TrimPrefix(r.URL.Path, "/api/friends/")
	parts := strings.Split(ids, "/")
	if len(parts) != 2 {
		writeErr(w, http.StatusBadRequest, "好友关系参数错误")
		return
	}
	selfID, err1 := strconv.Atoi(parts[0])
	friendID, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || selfID <= 0 || friendID <= 0 || selfID == friendID {
		writeErr(w, http.StatusBadRequest, "好友关系参数错误")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()

	res, err := tx.Exec(`DELETE FROM friend
		WHERE (self_id = ? AND friend_id = ?) OR (self_id = ? AND friend_id = ?)`,
		selfID, friendID, friendID, selfID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}

	_, err = tx.Exec(`UPDATE friend_apply SET status = 0
		WHERE ((from_uid = ? AND to_uid = ?) OR (from_uid = ? AND to_uid = ?)) AND status = 1`,
		selfID, friendID, friendID, selfID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	a.logOperation(r, operator, "friend", "delete", "friend", fmt.Sprintf("%d-%d", selfID, friendID), &selfID, fmt.Sprintf("删除好友关系 %d <-> %d", selfID, friendID), map[string]any{
		"self_id":   selfID,
		"friend_id": friendID,
	})
	writeJSON(w, map[string]any{"ok": affected(res) > 0})
}

func (a *app) getBG(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	var p bgPayload
	if !decodeJSON(w, r, &p) || p.URL == "" {
		return
	}
	req, _ := http.NewRequest(http.MethodGet, p.URL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "获取失败: "+err.Error())
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取失败")
		return
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/jpeg"
	}
	writeJSON(w, map[string]any{
		"ok":        true,
		"data_url":  "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(body),
		"final_url": resp.Request.URL.String(),
	})
}

func (a *app) friendApplies(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageLimit(r.URL.Query(), 80, 200)
	rows, err := a.queryMaps(`SELECT fa.id, fa.from_uid, fu.name AS from_name, fa.to_uid, tu.name AS to_name,
		fa.status, fa.descs, fa.back_name
		FROM friend_apply fa
		LEFT JOIN `+"`user`"+` fu ON fa.from_uid = fu.uid
		LEFT JOIN `+"`user`"+` tu ON fa.to_uid = tu.uid
		ORDER BY fa.id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var total int
	_ = a.db.QueryRow("SELECT COUNT(*) FROM friend_apply").Scan(&total)
	writeJSON(w, map[string]any{"items": rows, "total": total})
}

func (a *app) friendApplyByID(w http.ResponseWriter, r *http.Request, operator string) {
	id, _, ok := idFromPath(r.URL.Path, "/api/friend-applies/")
	if !ok || r.Method != http.MethodPatch {
		writeErr(w, 405, "方法不支持")
		return
	}
	var p statusPayload
	if !decodeJSON(w, r, &p) {
		return
	}

	switch p.Status {
	case 1: // 同意：更新状态 + 写入双向 friend 记录
		var fromUID, toUID int
		var backName string
		err := a.db.QueryRow("SELECT from_uid, to_uid, back_name FROM friend_apply WHERE id = ?", id).Scan(&fromUID, &toUID, &backName)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		tx, err := a.db.Begin()
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		defer tx.Rollback()

		_, err = tx.Exec("UPDATE friend_apply SET status = 1 WHERE id = ?", id)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		_, err = tx.Exec("INSERT IGNORE INTO friend(self_id, friend_id, back) VALUES (?, ?, ?)", toUID, fromUID, backName)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		_, err = tx.Exec("INSERT IGNORE INTO friend(self_id, friend_id, back) VALUES (?, ?, '')", fromUID, toUID)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if err := tx.Commit(); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		a.logOperation(r, operator, "friend_apply", "approve", "friend_apply", strconv.Itoa(id), nil, fmt.Sprintf("同意好友申请 ID %d (%d <-> %d)", id, fromUID, toUID), p)
		writeJSON(w, map[string]any{"ok": true})

	case 2: // 拒绝：只更新状态
		res, err := a.db.Exec("UPDATE friend_apply SET status = 2 WHERE id = ?", id)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		a.logOperation(r, operator, "friend_apply", "reject", "friend_apply", strconv.Itoa(id), nil, fmt.Sprintf("拒绝好友申请 ID %d", id), p)
		writeJSON(w, map[string]any{"ok": affected(res) > 0})

	default: // 0 = 取消处理：更新状态，如果之前是同意则删除好友关系
		var prevStatus int
		err := a.db.QueryRow("SELECT status FROM friend_apply WHERE id = ?", id).Scan(&prevStatus)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		tx, err := a.db.Begin()
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		defer tx.Rollback()

		_, err = tx.Exec("UPDATE friend_apply SET status = 0 WHERE id = ?", id)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if prevStatus == 1 {
			var fromUID, toUID int
			_ = tx.QueryRow("SELECT from_uid, to_uid FROM friend_apply WHERE id = ?", id).Scan(&fromUID, &toUID)
			_, err = tx.Exec("DELETE FROM friend WHERE (self_id = ? AND friend_id = ?) OR (self_id = ? AND friend_id = ?)", toUID, fromUID, fromUID, toUID)
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
		}
		if err := tx.Commit(); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		a.logOperation(r, operator, "friend_apply", "cancel", "friend_apply", strconv.Itoa(id), nil, fmt.Sprintf("取消处理好友申请 ID %d", id), p)
		writeJSON(w, map[string]any{"ok": true})
	}
}

func (a *app) starNotices(w http.ResponseWriter, r *http.Request, operator string) {
	switch r.Method {
	case http.MethodGet:
		q := trim(r.URL.Query().Get("q"), 0)
		limit, offset := pageLimit(r.URL.Query(), 50, 200)
		args := []any{}
		where := ""
		if q != "" {
			kw := "%" + q + "%"
			where = "WHERE title LIKE ? OR author LIKE ? OR content LIKE ?"
			args = append(args, kw, kw, kw)
		}
		args = append(args, limit, offset)
		rows, err := a.queryMaps("SELECT title, author, content FROM StarNotice "+where+" ORDER BY title ASC LIMIT ? OFFSET ?", args...)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		var total int
		countArgs := []any{}
		if q != "" {
			kw := "%" + q + "%"
			countArgs = append(countArgs, kw, kw, kw)
		}
		_ = a.db.QueryRow("SELECT COUNT(*) FROM StarNotice "+where, countArgs...).Scan(&total)
		writeJSON(w, map[string]any{"items": rows, "total": total})
	case http.MethodPost:
		var p starPayload
		if !decodeJSON(w, r, &p) {
			return
		}
		_, err := a.db.Exec("INSERT INTO StarNotice (title, author, content) VALUES (?, ?, ?)", p.Title, p.Author, p.Content)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		a.logOperation(r, operator, "star_notice", "create", "star_notice", p.Title, nil, "新增公告 "+p.Title, p)
		writeJSON(w, map[string]any{"ok": true})
	case http.MethodPatch:
		var p starPayload
		if !decodeJSON(w, r, &p) {
			return
		}
		title, author := r.URL.Query().Get("title"), r.URL.Query().Get("author")
		res, err := a.db.Exec("UPDATE StarNotice SET title = ?, author = ?, content = ? WHERE title = ? AND author = ?", p.Title, p.Author, p.Content, title, author)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		a.logOperation(r, operator, "star_notice", "update", "star_notice", title, nil, "编辑公告 "+title, p)
		writeJSON(w, map[string]any{"ok": affected(res) > 0})
	case http.MethodDelete:
		title, author := r.URL.Query().Get("title"), r.URL.Query().Get("author")
		res, err := a.db.Exec("DELETE FROM StarNotice WHERE title = ? AND author = ?", title, author)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		a.logOperation(r, operator, "star_notice", "delete", "star_notice", title, nil, "删除公告 "+title, nil)
		writeJSON(w, map[string]any{"ok": affected(res) > 0})
	default:
		writeErr(w, 405, "方法不支持")
	}
}

func (a *app) adminNotices(w http.ResponseWriter, r *http.Request, operator string) {
	switch r.Method {
	case http.MethodGet:
		q := trim(r.URL.Query().Get("q"), 0)
		limit, offset := pageLimit(r.URL.Query(), 80, 200)
		args := []any{}
		where := ""
		if q != "" {
			kw := "%" + q + "%"
			where = "WHERE title LIKE ? OR content LIKE ? OR level LIKE ?"
			args = append(args, kw, kw, kw)
		}
		args = append(args, limit, offset)
		rows, err := a.queryMaps("SELECT id, target_uid, title, content, level, delivered, create_time FROM admin_notice "+where+" ORDER BY id DESC LIMIT ? OFFSET ?", args...)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		var total int
		countArgs := []any{}
		if q != "" {
			kw := "%" + q + "%"
			countArgs = append(countArgs, kw, kw, kw)
		}
		_ = a.db.QueryRow("SELECT COUNT(*) FROM admin_notice "+where, countArgs...).Scan(&total)
		writeJSON(w, map[string]any{"items": rows, "total": total})
	case http.MethodPost:
		var p noticePayload
		if !decodeJSON(w, r, &p) {
			return
		}
		level := p.Level
		if level == "" {
			level = "info"
		}
		res, err := a.db.Exec("INSERT INTO admin_notice (target_uid, title, content, level) VALUES (?, ?, ?, ?)", p.TargetUID, p.Title, p.Content, level)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		id := int(lastID(res))
		a.logOperation(r, operator, "admin_notice", "create", "admin_notice", strconv.Itoa(id), p.TargetUID, fmt.Sprintf("创建后台通知 ID %d", id), p)
		writeJSON(w, map[string]any{"ok": true, "id": id})
	default:
		writeErr(w, 405, "方法不支持")
	}
}

func (a *app) adminNoticeByID(w http.ResponseWriter, r *http.Request, operator string) {
	id, rest, ok := idFromPath(r.URL.Path, "/api/admin-notices/")
	if !ok {
		writeErr(w, 404, "通知不存在")
		return
	}
	if rest == "/delivered" && r.Method == http.MethodPatch {
		res, err := a.db.Exec("UPDATE admin_notice SET delivered = 1 WHERE id = ?", id)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		a.logOperation(r, operator, "admin_notice", "delivered", "admin_notice", strconv.Itoa(id), nil, fmt.Sprintf("标记通知已处理 ID %d", id), nil)
		writeJSON(w, map[string]any{"ok": affected(res) > 0})
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var p noticePayload
		if !decodeJSON(w, r, &p) {
			return
		}
		res, err := a.db.Exec("UPDATE admin_notice SET target_uid = ?, title = ?, content = ?, level = ?, delivered = ? WHERE id = ?", p.TargetUID, p.Title, p.Content, p.Level, p.Delivered, id)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		a.logOperation(r, operator, "admin_notice", "update", "admin_notice", strconv.Itoa(id), p.TargetUID, fmt.Sprintf("编辑后台通知 ID %d", id), p)
		writeJSON(w, map[string]any{"ok": affected(res) > 0})
	case http.MethodDelete:
		res, err := a.db.Exec("DELETE FROM admin_notice WHERE id = ?", id)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		a.logOperation(r, operator, "admin_notice", "delete", "admin_notice", strconv.Itoa(id), nil, fmt.Sprintf("删除后台通知 ID %d", id), nil)
		writeJSON(w, map[string]any{"ok": affected(res) > 0})
	default:
		writeErr(w, 405, "方法不支持")
	}
}
