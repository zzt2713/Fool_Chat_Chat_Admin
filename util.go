package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func pageLimit(v url.Values, defaultLimit, maxLimit int) (int, int) {
	page := intParam(v.Get("page"), 1)
	limit := intParam(v.Get("limit"), defaultLimit)
	limit = int(math.Max(1, math.Min(float64(maxLimit), float64(limit))))
	return limit, (page - 1) * limit
}

func intParam(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

func idFromPath(path, prefix string) (int, string, bool) {
	s := strings.TrimPrefix(path, prefix)
	parts := strings.SplitN(s, "/", 2)
	id, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", false
	}
	rest := ""
	if len(parts) > 1 {
		rest = "/" + parts[1]
	}
	return id, rest, true
}

func addEquals(conditions *[]string, args *[]any, field, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	*conditions = append(*conditions, field+" = ?")
	*args = append(*args, value)
}

func whereSQL(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(conditions, " AND ")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeErr(w, 400, "JSON 参数错误")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(data)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"detail": msg})
}

func sha256Hex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func randomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

func affected(res sql.Result) int64 {
	n, err := res.RowsAffected()
	if err != nil {
		return 0
	}
	return n
}

func lastID(res sql.Result) int64 {
	n, err := res.LastInsertId()
	if err != nil {
		return 0
	}
	return n
}

func trim(s string, max int) string {
	if max > 0 && len(s) > max {
		return s[:max]
	}
	return s
}

var _ = errors.New
