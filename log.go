package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

func (a *app) logOperation(r *http.Request, operator, module, action, targetType, targetID string, targetUID *int, summary string, detail any) {
	b, _ := json.Marshal(detail)
	ip := r.RemoteAddr
	if host := r.Header.Get("X-Forwarded-For"); host != "" {
		ip = strings.Split(host, ",")[0]
	}
	_, err := a.db.Exec(`INSERT INTO admin_operation_log
		(module, action, target_type, target_id, target_uid, operator, summary, detail_json, ip, user_agent)
		VALUES (?, ?, ?, ?, ?, ?, ?, CAST(? AS JSON), ?, ?)`,
		module, action, targetType, targetID, targetUID, operator, summary, string(b), ip, trim(r.UserAgent(), 255))
	if err != nil {
		log.Printf("[WARN] write log failed: %v", err)
	}
}
