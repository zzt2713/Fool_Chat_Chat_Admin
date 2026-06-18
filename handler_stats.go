package main

import (
	"net/http"
	"strings"
)

func (a *app) summary(w http.ResponseWriter, r *http.Request) {
	rows, err := a.queryMaps(`SELECT
		(SELECT COUNT(*) FROM ` + "`user`" + `) AS users,
		(SELECT COUNT(*) FROM ` + "`dynamic`" + `) AS dynamics,
		(SELECT COUNT(*) FROM ` + "`dynamic`" + ` WHERE DATE(create_time) = CURDATE()) AS today_dynamics,
		(SELECT COUNT(*) FROM ` + "`dynamic`" + ` WHERE status = 1) AS pending_dynamics,
		(SELECT COUNT(*) FROM admin_operation_log WHERE module = 'user' AND action = 'create' AND DATE(create_time) = CURDATE()) AS today_users,
		(SELECT COUNT(*) FROM admin_operation_log WHERE DATE(create_time) = CURDATE()) AS today_operations,
		(SELECT COUNT(*) FROM admin_operation_log) AS total_operations,
		(SELECT COUNT(*) FROM friend_apply WHERE status = 0) AS pending_applies,
		(SELECT COUNT(*) FROM admin_notice) AS notices,
		(SELECT COUNT(*) FROM ai_chat_message WHERE role = 'user' AND DATE(create_time) = CURDATE()) AS today_ai_chats`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(rows) == 0 {
		writeJSON(w, map[string]any{})
		return
	}
	recentLogins, _ := a.queryMaps(`SELECT operator AS user, ip, create_time
		FROM admin_operation_log
		WHERE module = 'auth' AND action = 'login'
		ORDER BY id DESC LIMIT 3`)
	recentErrors, _ := a.queryMaps(`SELECT module, action, operator AS user, summary, create_time
		FROM admin_operation_log
		WHERE action IN ('login_failed', 'login_denied')
		   OR action LIKE '%error%'
		   OR action LIKE '%failed%'
		ORDER BY id DESC LIMIT 3`)
	data := rows[0]
	data["recent_logins"] = recentLogins
	data["recent_errors"] = recentErrors
	writeJSON(w, data)
}

func (a *app) analytics(w http.ResponseWriter, r *http.Request) {
	trend, err := a.queryMaps(`SELECT DATE(create_time) AS day, COUNT(*) AS count
		FROM ` + "`dynamic`" + `
		WHERE create_time >= DATE_SUB(CURDATE(), INTERVAL 13 DAY)
		GROUP BY DATE(create_time)
		ORDER BY day ASC`)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	applies, _ := a.queryMaps("SELECT status, COUNT(*) AS count FROM friend_apply GROUP BY status")
	notices, _ := a.queryMaps("SELECT COUNT(*) AS total, SUM(CASE WHEN delivered = 1 THEN 1 ELSE 0 END) AS delivered FROM admin_notice")
	var notice any = map[string]any{"total": 0, "delivered": 0}
	if len(notices) > 0 {
		notice = notices[0]
	}
	writeJSON(w, map[string]any{"dynamic_trend": trend, "apply_stats": applies, "notice_stats": notice})
}

func (a *app) logs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, "方法不支持")
		return
	}
	q := r.URL.Query()
	conditions := []string{}
	args := []any{}
	if s := strings.TrimSpace(q.Get("q")); s != "" {
		kw := "%" + s + "%"
		conditions = append(conditions, "(operator LIKE ? OR summary LIKE ? OR module LIKE ? OR action LIKE ?)")
		args = append(args, kw, kw, kw, kw)
	}
	addEquals(&conditions, &args, "module", q.Get("module"))
	addEquals(&conditions, &args, "action", q.Get("action"))
	addEquals(&conditions, &args, "operator", q.Get("operator"))
	if s := q.Get("start_date"); s != "" {
		conditions = append(conditions, "create_time >= ?")
		args = append(args, s)
	}
	if s := q.Get("end_date"); s != "" {
		conditions = append(conditions, "create_time < DATE_ADD(?, INTERVAL 1 DAY)")
		args = append(args, s)
	}
	where := whereSQL(conditions)
	limit, offset := pageLimit(q, 30, 200)
	args = append(args, limit, offset)
	rows, err := a.queryMaps(`SELECT id, module, action, target_type, target_id, target_uid,
		operator AS `+"`user`"+`, summary, detail_json, ip, create_time
		FROM admin_operation_log `+where+`
		ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, rows)
}

func (a *app) logOperators(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, "方法不支持")
		return
	}
	rows, err := a.queryMaps(`SELECT operator, COUNT(*) AS cnt
		FROM admin_operation_log
		WHERE operator <> ''
		GROUP BY operator
		ORDER BY cnt DESC, operator ASC
		LIMIT 200`)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, rows)
}
