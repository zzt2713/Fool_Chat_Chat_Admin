package main

import (
	"fmt"
	"net/http"
	"strconv"
)

type dynamicPayload struct {
	UID       int    `json:"uid"`
	Content   string `json:"content"`
	LikeCount int    `json:"like_count"`
}

type statusPayload struct {
	Status int `json:"status"`
}

func (a *app) dynamics(w http.ResponseWriter, r *http.Request, operator string) {
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		conditions := []string{}
		args := []any{}
		if s := trim(q.Get("q"), 0); s != "" {
			kw := "%" + s + "%"
			conditions = append(conditions, "(u.name LIKE ? OR u.nick LIKE ? OR d.content LIKE ? OR CAST(d.uid AS CHAR) LIKE ?)")
			args = append(args, kw, kw, kw, kw)
		}
		if s := q.Get("status"); s == "0" || s == "1" || s == "2" {
			conditions = append(conditions, "d.status = ?")
			args = append(args, s)
		}
		limit, offset := pageLimit(q, 50, 200)
		args = append(args, limit, offset)
		rows, err := a.queryMaps(`SELECT d.id, d.uid, u.name, u.nick, d.content, d.like_count, d.status, d.create_time
			FROM `+"`dynamic`"+` d LEFT JOIN `+"`user`"+` u ON d.uid = u.uid `+whereSQL(conditions)+`
			ORDER BY d.id DESC LIMIT ? OFFSET ?`, args...)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, rows)
	case http.MethodPost:
		var p dynamicPayload
		if !decodeJSON(w, r, &p) {
			return
		}
		res, err := a.db.Exec("INSERT INTO `dynamic` (uid, content) VALUES (?, ?)", p.UID, p.Content)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		id := int(lastID(res))
		a.logOperation(r, operator, "dynamic", "create", "dynamic", strconv.Itoa(id), &p.UID, fmt.Sprintf("发布动态 ID %d", id), p)
		writeJSON(w, map[string]any{"ok": true, "id": id})
	default:
		writeErr(w, 405, "方法不支持")
	}
}

func (a *app) dynamicByID(w http.ResponseWriter, r *http.Request, operator string) {
	id, rest, ok := idFromPath(r.URL.Path, "/api/dynamics/")
	if !ok {
		writeErr(w, 404, "动态不存在")
		return
	}
	if rest == "/status" && r.Method == http.MethodPatch {
		var p statusPayload
		if !decodeJSON(w, r, &p) {
			return
		}
		if p.Status < 0 || p.Status > 2 {
			writeErr(w, 400, "动态状态不支持")
			return
		}
		res, err := a.db.Exec("UPDATE `dynamic` SET status = ? WHERE id = ?", p.Status, id)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		a.logOperation(r, operator, "dynamic", "update_status", "dynamic", strconv.Itoa(id), nil, fmt.Sprintf("动态 ID %d 状态改为 %d", id, p.Status), p)
		writeJSON(w, map[string]any{"ok": affected(res) > 0})
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var p dynamicPayload
		if !decodeJSON(w, r, &p) {
			return
		}
		res, err := a.db.Exec("UPDATE `dynamic` SET content = ?, like_count = ? WHERE id = ?", p.Content, p.LikeCount, id)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		a.logOperation(r, operator, "dynamic", "update", "dynamic", strconv.Itoa(id), nil, fmt.Sprintf("编辑动态 ID %d", id), p)
		writeJSON(w, map[string]any{"ok": affected(res) > 0})
	case http.MethodDelete:
		res, err := a.db.Exec("DELETE FROM `dynamic` WHERE id = ?", id)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		a.logOperation(r, operator, "dynamic", "delete", "dynamic", strconv.Itoa(id), nil, fmt.Sprintf("删除动态 ID %d", id), nil)
		writeJSON(w, map[string]any{"ok": affected(res) > 0})
	default:
		writeErr(w, 405, "方法不支持")
	}
}
