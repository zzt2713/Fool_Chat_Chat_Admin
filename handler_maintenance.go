package main

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type maintenanceDataset struct {
	Type     string
	Filename string
	Query    string
}

var maintenanceDatasets = map[string]maintenanceDataset{
	"users": {
		Type:     "users",
		Filename: "users.csv",
		Query: "SELECT uid, name, email, nick, sex, role, status, `desc` " +
			"FROM `user` ORDER BY uid ASC",
	},
	"dynamics": {
		Type:     "dynamics",
		Filename: "dynamics.csv",
		Query: "SELECT d.id, d.uid, u.name, d.content, d.like_count, d.status, d.create_time " +
			"FROM `dynamic` d LEFT JOIN `user` u ON d.uid = u.uid ORDER BY d.id DESC",
	},
	"logs": {
		Type:     "logs",
		Filename: "operation_logs.csv",
		Query: "SELECT id, module, action, operator, summary, ip, create_time " +
			"FROM admin_operation_log ORDER BY id DESC",
	},
}

func (a *app) maintenance(w http.ResponseWriter, r *http.Request, operator string) {
	switch {
	case r.URL.Path == "/api/maintenance/summary":
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
			return
		}
		a.maintenanceSummary(w, r)
	case r.URL.Path == "/api/maintenance/export":
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
			return
		}
		a.maintenanceExport(w, r, operator)
	default:
		writeErr(w, http.StatusNotFound, "接口不存在")
	}
}

func (a *app) maintenanceSummary(w http.ResponseWriter, r *http.Request) {
	rows, err := a.queryMaps(`SELECT
		(SELECT COUNT(*) FROM `+"`user`"+`) AS users,
		(SELECT COUNT(*) FROM `+"`dynamic`"+`) AS dynamics,
		(SELECT COUNT(*) FROM admin_operation_log) AS logs,
		(SELECT COUNT(*) FROM friend) AS friends,
		(SELECT COUNT(*) FROM friend_apply) AS friend_applies,
		(SELECT COUNT(*) FROM admin_notice) AS admin_notices,
		(SELECT COUNT(*) FROM StarNotice) AS star_notices,
		(SELECT COUNT(*) FROM ai_chat_message) AS ai_messages`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	counts := map[string]any{}
	if len(rows) > 0 {
		counts = rows[0]
	}
	writeJSON(w, map[string]any{
		"database": map[string]any{
			"host":   env("DB_HOST", a.cfg.DBHost),
			"port":   env("DB_PORT", a.cfg.DBPort),
			"name":   env("DB_NAME", a.cfg.DBName),
			"user":   env("DB_USER", a.cfg.DBUser),
			"driver": "mysql",
		},
		"counts":     counts,
		"exportable": []string{"users", "dynamics", "logs", "all"},
		"checked_at": time.Now().Format("2006-01-02 15:04:05"),
	})
}

func (a *app) maintenanceExport(w http.ResponseWriter, r *http.Request, operator string) {
	exportType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
	if exportType == "" {
		exportType = "all"
	}
	if exportType == "all" {
		a.exportAllCSVZip(w, r, operator)
		return
	}
	ds, ok := maintenanceDatasets[exportType]
	if !ok {
		writeErr(w, http.StatusBadRequest, "导出类型不支持")
		return
	}
	body, err := a.queryCSVBytes(ds.Query)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, ds.Filename))
	_, _ = w.Write(body)
	a.logOperation(r, operator, "maintenance", "export", exportType, exportType, nil, "导出数据备份："+exportType, nil)
}

func (a *app) exportAllCSVZip(w http.ResponseWriter, r *http.Request, operator string) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, key := range []string{"users", "dynamics", "logs"} {
		ds := maintenanceDatasets[key]
		body, err := a.queryCSVBytes(ds.Query)
		if err != nil {
			_ = zw.Close()
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		f, err := zw.Create(ds.Filename)
		if err != nil {
			_ = zw.Close()
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, err := f.Write(body); err != nil {
			_ = zw.Close()
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := zw.Close(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="fool_chat_backup_csv.zip"`)
	_, _ = w.Write(buf.Bytes())
	a.logOperation(r, operator, "maintenance", "export_all", "backup", "all", nil, "导出全部数据备份 CSV 压缩包", nil)
}

func (a *app) queryCSVBytes(query string, args ...any) ([]byte, error) {
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(&buf)
	if err := writer.Write(cols); err != nil {
		return nil, err
	}
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		record := make([]string, len(cols))
		for i, v := range raw {
			record[i] = csvValue(v)
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	writer.Flush()
	return buf.Bytes(), writer.Error()
}

func csvValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case time.Time:
		return x.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprint(x)
	}
}
