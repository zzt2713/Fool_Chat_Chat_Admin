package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var errVerifyFailed = errors.New("验证码服务返回失败")

type passwordResetPayload struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Code     string `json:"code"`
	Password string `json:"password"`
}

type resetAccount struct {
	UID   int
	Name  string
	Email string
	Role  int
}

func (a *app) passwordReset(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/password-reset/lookup":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "方法不允许")
			return
		}
		a.passwordResetLookup(w, r)
	case "/api/password-reset/send":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "方法不允许")
			return
		}
		a.passwordResetSend(w, r)
	case "/api/password-reset/verify":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "方法不允许")
			return
		}
		a.passwordResetVerify(w, r)
	case "/api/password-reset/reset":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "方法不允许")
			return
		}
		a.passwordResetConfirm(w, r)
	default:
		writeErr(w, http.StatusNotFound, "接口不存在")
	}
}

func (a *app) passwordResetLookup(w http.ResponseWriter, r *http.Request) {
	var p passwordResetPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	account, ok := a.findResetAccount(strings.TrimSpace(p.Name))
	if !ok {
		writeErr(w, http.StatusBadRequest, "账号不存在或未绑定邮箱")
		return
	}
	writeJSON(w, map[string]any{
		"name":         account.Name,
		"masked_email": maskEmail(account.Email),
	})
}

func (a *app) passwordResetSend(w http.ResponseWriter, r *http.Request) {
	var p passwordResetPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	account, ok := a.findResetAccount(strings.TrimSpace(p.Name))
	if !ok {
		writeErr(w, http.StatusBadRequest, "账号不存在或未绑定邮箱")
		return
	}
	inputEmail := strings.ToLower(strings.TrimSpace(p.Email))
	if inputEmail == "" {
		writeErr(w, http.StatusBadRequest, "请填写完整邮箱")
		return
	}
	if inputEmail != strings.ToLower(strings.TrimSpace(account.Email)) {
		writeErr(w, http.StatusBadRequest, "邮箱与账号绑定的邮箱不一致")
		return
	}
	if err := a.sendVerifyCode(account.Email); err != nil {
		log.Printf("[password-reset] send verify code failed: email=%s err=%v", account.Email, err)
		writeErr(w, http.StatusBadGateway, "验证码发送失败："+err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "masked_email": maskEmail(account.Email)})
}

func (a *app) passwordResetVerify(w http.ResponseWriter, r *http.Request) {
	var p passwordResetPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	name := strings.TrimSpace(p.Name)
	email := strings.ToLower(strings.TrimSpace(p.Email))
	code := strings.ToUpper(strings.TrimSpace(p.Code))
	if name == "" || email == "" || code == "" {
		writeErr(w, http.StatusBadRequest, "请填写账号、邮箱和验证码")
		return
	}
	account, ok := a.findResetAccount(name)
	if !ok {
		writeErr(w, http.StatusBadRequest, "账号不存在或未绑定邮箱")
		return
	}
	if email != strings.ToLower(strings.TrimSpace(account.Email)) {
		writeErr(w, http.StatusBadRequest, "邮箱与账号绑定的邮箱不一致")
		return
	}
	if !a.checkResetCode(account.Email, code) {
		writeErr(w, http.StatusBadRequest, "验证码错误或已过期")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (a *app) passwordResetConfirm(w http.ResponseWriter, r *http.Request) {
	var p passwordResetPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	p.Name = strings.TrimSpace(p.Name)
	p.Email = strings.ToLower(strings.TrimSpace(p.Email))
	p.Code = strings.ToUpper(strings.TrimSpace(p.Code))
	p.Password = strings.TrimSpace(p.Password)
	if p.Name == "" || p.Code == "" || len(p.Password) < 3 {
		writeErr(w, http.StatusBadRequest, "请完整填写账号、验证码和新密码")
		return
	}
	account, ok := a.findResetAccount(p.Name)
	if !ok {
		writeErr(w, http.StatusBadRequest, "账号不存在或未绑定邮箱")
		return
	}
	if p.Email != "" && p.Email != strings.ToLower(strings.TrimSpace(account.Email)) {
		writeErr(w, http.StatusBadRequest, "邮箱与账号绑定的邮箱不一致")
		return
	}
	if !a.checkResetCode(account.Email, p.Code) {
		writeErr(w, http.StatusBadRequest, "验证码错误或已过期")
		return
	}
	if _, err := a.db.Exec("UPDATE `user` SET pwd = ? WHERE uid = ?", sha256Hex(p.Password), account.UID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.deleteResetCode(account.Email)
	a.logOperation(r, account.Name, "auth", "reset_password", "user", fmt.Sprint(account.UID), &account.UID, "找回密码重置账号密码", nil)
	writeJSON(w, map[string]any{"ok": true})
}

func (a *app) findResetAccount(name string) (resetAccount, bool) {
	var account resetAccount
	if name == "" {
		return account, false
	}
	err := a.db.QueryRow("SELECT uid, name, email, role FROM `user` WHERE name = ? LIMIT 1", name).
		Scan(&account.UID, &account.Name, &account.Email, &account.Role)
	if err != nil || errors.Is(err, sql.ErrNoRows) {
		return account, false
	}
	if strings.TrimSpace(account.Email) == "" || account.Role <= 0 {
		return account, false
	}
	return account, true
}

func (a *app) redisClient() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     env("REDIS_ADDR", a.cfg.RedisHost+":"+a.cfg.RedisPort),
		Password: env("REDIS_PASSWORD", a.cfg.RedisPassword),
		DB:       0,
	})
}

func (a *app) checkResetCode(email, code string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cli := a.redisClient()
	defer cli.Close()
	value, err := cli.Get(ctx, "code_"+email).Result()
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(value), code)
}

func (a *app) deleteResetCode(email string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cli := a.redisClient()
	defer cli.Close()
	_ = cli.Del(ctx, "code_"+email).Err()
}

func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return "*******"
	}
	local := parts[0]
	prefix := local
	if len([]rune(prefix)) > 2 {
		prefix = string([]rune(prefix)[:2])
	}
	if prefix == "" {
		prefix = "*"
	}
	return prefix + "*******@" + parts[1]
}
