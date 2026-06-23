package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type userPayload struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Nick     string `json:"nick"`
	Desc     string `json:"desc"`
	Sex      string `json:"sex"`
	Icon     string `json:"icon"`
}

type passwordPayload struct {
	Password string `json:"password"`
}

type rolePayload struct {
	Role int `json:"role"`
}

func (a *app) getOperatorAndTarget(operator string, targetUID int) (int, int, int, error) {
	var operatorUID, operatorRole int
	if err := a.db.QueryRow("SELECT uid, role FROM `user` WHERE name = ? LIMIT 1", operator).Scan(&operatorUID, &operatorRole); err != nil {
		return 0, 0, 0, err
	}

	var targetRole int
	if err := a.db.QueryRow("SELECT role FROM `user` WHERE uid = ? LIMIT 1", targetUID).Scan(&targetRole); err != nil {
		return 0, 0, 0, err
	}

	return operatorUID, operatorRole, targetRole, nil
}

func (a *app) users(w http.ResponseWriter, r *http.Request, operator string) {
	switch r.Method {
	case http.MethodGet:
		q := trim(r.URL.Query().Get("q"), 0)
		limit, offset := pageLimit(r.URL.Query(), 50, 200)
		args := []any{}
		where := ""
		if q != "" {
			kw := "%" + q + "%"
			where = "WHERE name LIKE ? OR email LIKE ? OR nick LIKE ? OR CAST(uid AS CHAR) LIKE ?"
			args = append(args, kw, kw, kw, kw)
		}
		args = append(args, limit, offset)
		rows, err := a.queryMaps("SELECT id, uid, name, email, nick, `desc`, sex, icon, role, status FROM `user` "+where+" ORDER BY role DESC, uid ASC LIMIT ? OFFSET ?", args...)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		var total int
		countArgs := []any{}
		if q != "" {
			kw := "%" + q + "%"
			countArgs = append(countArgs, kw, kw, kw, kw)
		}
		_ = a.db.QueryRow("SELECT COUNT(*) FROM `user` "+where, countArgs...).Scan(&total)
		writeJSON(w, map[string]any{"items": rows, "total": total})
	case http.MethodPost:
		var p userPayload
		if !decodeJSON(w, r, &p) {
			return
		}
		hash := sha256Hex(p.Password)
		var result int
		if _, err := a.db.Exec("CALL reg_user(?, ?, ?, @result)", p.Name, p.Email, hash); err != nil {
			if strings.Contains(err.Error(), "Duplicate entry") {
				writeErr(w, 400, "账号或邮箱已存在")
				return
			}
			writeErr(w, 400, err.Error())
			return
		}
		if err := a.db.QueryRow("SELECT @result").Scan(&result); err != nil || result <= 0 {
			writeErr(w, 400, "账号或邮箱已存在")
			return
		}
		a.logOperation(r, operator, "user", "create", "user", strconv.Itoa(result), &result, fmt.Sprintf("创建账号 %s(%d)", p.Name, result), p)
		writeJSON(w, map[string]any{"ok": true, "uid": result})
	default:
		writeErr(w, 405, "方法不支持")
	}
}

func (a *app) userByID(w http.ResponseWriter, r *http.Request, operator string) {
	id, rest, ok := idFromPath(r.URL.Path, "/api/users/")
	if !ok {
		writeErr(w, 404, "用户不存在")
		return
	}
	if rest == "/password" && r.Method == http.MethodPatch {
		var p passwordPayload
		if !decodeJSON(w, r, &p) {
			return
		}
		operatorUID, operatorRole, targetRole, err := a.getOperatorAndTarget(operator, id)
		if err != nil {
			writeErr(w, 404, "用户不存在")
			return
		}
		if operatorUID != id && targetRole >= operatorRole{
			writeErr(w, 403, "不能重置权限高于或等于自己的账号密码")
			return
		}
		res, err := a.db.Exec("UPDATE `user` SET pwd = ? WHERE uid = ?", sha256Hex(p.Password), id)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if affected(res) > 0 {
			a.logOperation(r, operator, "user", "reset_password", "user", strconv.Itoa(id), &id, fmt.Sprintf("重置账号密码 UID %d", id), nil)
		}
		writeJSON(w, map[string]any{"ok": affected(res) > 0})
		return
	}
	if rest == "/role" && r.Method == http.MethodPatch {
		var p rolePayload
		if !decodeJSON(w, r, &p) {
			return
		}
		if p.Role < 0 || p.Role > 1 {
			writeErr(w, 400, "角色不支持")
			return
		}
		var operatorUID, operatorRole int
		if err := a.db.QueryRow("SELECT uid, role FROM `user` WHERE name = ? LIMIT 1", operator).Scan(&operatorUID, &operatorRole); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if operatorRole != 2 {
			writeErr(w, 403, "只有超级管理员可以任命或取消任命管理员")
			return
		}
		if operatorUID == id {
			writeErr(w, 400, "不能修改自己的角色")
			return
		}
		var targetRole int
		if err := a.db.QueryRow("SELECT role FROM `user` WHERE uid = ? LIMIT 1", id).Scan(&targetRole); err != nil {
			writeErr(w, 404, "用户不存在")
			return
		}
		if targetRole == 2 {
			writeErr(w, 403, "不能修改超级管理员角色")
			return
		}
		res, err := a.db.Exec("UPDATE `user` SET role = ? WHERE uid = ?", p.Role, id)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if affected(res) > 0 {
			a.logOperation(r, operator, "user", "appoint_role", "user", strconv.Itoa(id), &id, fmt.Sprintf("任命账号角色 UID %d => %d", id, p.Role), p)
		}
		writeJSON(w, map[string]any{"ok": affected(res) > 0})
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var p userPayload
		if !decodeJSON(w, r, &p) {
			return
		}
		operatorUID, operatorRole, targetRole, err := a.getOperatorAndTarget(operator, id)
		if err != nil {
			writeErr(w, 404, "用户不存在")
			return
		}
		if operatorUID != id && targetRole >= operatorRole {
			writeErr(w, 403, "不能编辑权限高于或等于自己的账号")
			return
		}
		res, err := a.db.Exec("UPDATE `user` SET name=?, email=?, nick=?, `desc`=?, sex=?, icon=? WHERE uid=?", p.Name, p.Email, p.Nick, p.Desc, p.Sex, p.Icon, id)
		if err != nil {
			if strings.Contains(err.Error(), "Duplicate entry") {
				writeErr(w, 400, "账号或邮箱已存在")
				return
			}
			writeErr(w, 500, err.Error())
			return
		}
		a.logOperation(r, operator, "user", "update", "user", strconv.Itoa(id), &id, fmt.Sprintf("编辑账号资料 UID %d", id), p)
		writeJSON(w, map[string]any{"ok": affected(res) >= 0})
	case http.MethodDelete:
		var operatorUID, operatorRole int
		if err := a.db.QueryRow("SELECT uid, role FROM `user` WHERE name = ? LIMIT 1", operator).Scan(&operatorUID, &operatorRole); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if operatorUID == id {
			writeErr(w, 403, "不能删除自己的账号")
			return
		}
		var targetRole int
		if err := a.db.QueryRow("SELECT role FROM `user` WHERE uid = ? LIMIT 1", id).Scan(&targetRole); err != nil {
			writeErr(w, 404, "用户不存在")
			return
		}
		if targetRole >= operatorRole {
			writeErr(w, 403, "不能删除权限高于或等于自己的账号")
			return
		}
		if r.Header.Get("X-Delete-Password") != a.cfg.DeletePassword {
			writeErr(w, 403, "二级验证密码错误")
			return
		}
		tx, err := a.db.Begin()
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		stmts := []string{
			"DELETE FROM friend_apply WHERE from_uid = ? OR to_uid = ?",
			"DELETE FROM friend WHERE self_id = ? OR friend_id = ?",
			"DELETE FROM `dynamic` WHERE uid = ?",
			"DELETE FROM admin_notice WHERE target_uid = ?",
			"DELETE FROM `user` WHERE uid = ?",
		}
		for i, stmt := range stmts {
			if i < 2 {
				_, err = tx.Exec(stmt, id, id)
			} else {
				_, err = tx.Exec(stmt, id)
			}
			if err != nil {
				_ = tx.Rollback()
				writeErr(w, 500, err.Error())
				return
			}
		}
		if err := tx.Commit(); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		a.logOperation(r, operator, "user", "delete", "user", strconv.Itoa(id), &id, fmt.Sprintf("删除账号 UID %d", id), nil)
		writeJSON(w, map[string]any{"ok": true})
	default:
		writeErr(w, 405, "方法不支持")
	}
}
