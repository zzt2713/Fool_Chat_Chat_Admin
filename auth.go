package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type loginPayload struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	var p loginPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	row := map[string]any{}
	hash := sha256Hex(p.Password)
	err := a.db.QueryRow("SELECT uid, name, email, role FROM `user` WHERE name = ? AND pwd = ?", p.Name, hash).
		Scan(ptr(&row, "uid"), ptr(&row, "name"), ptr(&row, "email"), ptr(&row, "role"))
	if err == sql.ErrNoRows {
		a.logOperation(r, p.Name, "auth", "login_failed", "user", "", nil, "login failed: "+p.Name, nil)
		writeErr(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	role := fmt.Sprint(row["role"])
	if role != "1" && role != "2" {
		a.logOperation(r, p.Name, "auth", "login_denied", "user", fmt.Sprint(row["uid"]), nil, "login denied: "+p.Name, nil)
		writeErr(w, http.StatusForbidden, "无管理权限")
		return
	}
	token := a.newSessionToken(fmt.Sprint(row["name"]))
	a.mu.Lock()
	a.sessions[token] = fmt.Sprint(row["name"])
	a.mu.Unlock()
	a.logOperation(r, fmt.Sprint(row["name"]), "auth", "login", "user", fmt.Sprint(row["uid"]), nil, "admin login: "+fmt.Sprint(row["name"]), nil)
	http.SetCookie(w, &http.Cookie{Name: "token", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 259200})
	writeJSON(w, map[string]any{"ok": true, "uid": row["uid"], "name": row["name"], "email": row["email"], "role": row["role"]})
}

func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("token"); err == nil {
		a.mu.Lock()
		delete(a.sessions, c.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "token", Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, map[string]any{"ok": true})
}

func (a *app) currentUser(r *http.Request) (string, bool) {
	c, err := r.Cookie("token")
	if err != nil {
		return "", false
	}
	a.mu.RLock()
	u, ok := a.sessions[c.Value]
	a.mu.RUnlock()
	if ok {
		return u, true
	}
	u, ok = a.verifySessionToken(c.Value)
	if !ok || !a.isAdminUser(u) {
		return "", false
	}
	a.mu.Lock()
	a.sessions[c.Value] = u
	a.mu.Unlock()
	return u, true
}

func (a *app) newSessionToken(user string) string {
	exp := time.Now().Add(72 * time.Hour).Unix()
	payload := fmt.Sprintf("%s|%d|%s", user, exp, randomToken())
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encoded + "." + a.sessionSignature(encoded)
}

func (a *app) verifySessionToken(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(parts[1]), []byte(a.sessionSignature(parts[0]))) {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	fields := strings.Split(string(raw), "|")
	if len(fields) != 3 {
		return "", false
	}
	exp, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", false
	}
	return fields[0], fields[0] != ""
}

func (a *app) sessionSignature(encodedPayload string) string {
	key := sha256.Sum256([]byte(a.cfg.SessionKey + "|" + a.cfg.DBName + "|" + a.cfg.DBPassword))
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(encodedPayload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *app) isAdminUser(name string) bool {
	var role string
	err := a.db.QueryRow("SELECT role FROM `user` WHERE name = ? LIMIT 1", name).Scan(&role)
	return err == nil && (role == "1" || role == "2")
}
