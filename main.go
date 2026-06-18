package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type app struct {
	db       *sql.DB
	tpl      *template.Template
	sessions map[string]string
	mu       sync.RWMutex
	cfg      appConfig
}

func main() {
	cfg := loadConfig("config.yaml")
	db, err := openDB(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	a := &app{
		db:       db,
		tpl:      template.Must(template.ParseFiles("templates/index.html")),
		sessions: map[string]string{},
		cfg:      cfg,
	}
	if err := a.ensureSchema(); err != nil {
		log.Printf("[WARN] schema init failed: %v", err)
	}

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", a.index)
	http.HandleFunc("/api/", a.api)

	addr := env("APP_ADDR", cfg.AppAddr)
	log.Printf("Fool Chat Go Admin running at http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func openDB(cfg appConfig) (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Local",
		env("DB_USER", cfg.DBUser),
		env("DB_PASSWORD", cfg.DBPassword),
		env("DB_HOST", cfg.DBHost),
		env("DB_PORT", cfg.DBPort),
		env("DB_NAME", cfg.DBName),
	)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	return db, db.Ping()
}

func (a *app) ensureSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS admin_notice (
			id BIGINT NOT NULL AUTO_INCREMENT,
			target_uid INT NULL DEFAULT NULL,
			title VARCHAR(80) NOT NULL DEFAULT '',
			content TEXT NULL,
			level VARCHAR(20) NOT NULL DEFAULT 'info',
			delivered TINYINT NOT NULL DEFAULT 0,
			create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			INDEX idx_target_uid (target_uid),
			INDEX idx_delivered (delivered),
			INDEX idx_create_time (create_time)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS admin_operation_log (
			id BIGINT NOT NULL AUTO_INCREMENT,
			module VARCHAR(40) NOT NULL DEFAULT '',
			action VARCHAR(40) NOT NULL DEFAULT '',
			target_type VARCHAR(40) NOT NULL DEFAULT '',
			target_id VARCHAR(80) NULL DEFAULT '',
			target_uid INT NULL DEFAULT NULL,
			operator VARCHAR(80) NOT NULL DEFAULT 'admin',
			summary VARCHAR(255) NOT NULL DEFAULT '',
			detail_json JSON NULL,
			ip VARCHAR(64) NULL DEFAULT '',
			user_agent VARCHAR(255) NULL DEFAULT '',
			create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			INDEX idx_module (module),
			INDEX idx_action (action),
			INDEX idx_target_uid (target_uid),
			INDEX idx_create_time (create_time)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}
	for _, stmt := range stmts {
		if _, err := a.db.Exec(stmt); err != nil {
			return err
		}
	}
	a.ensureAIChatSchema()
	return nil
}

func (a *app) ensureAIChatSchema() {
	_, _ = a.db.Exec(`CREATE TABLE IF NOT EXISTS ai_chat_message (
		id BIGINT NOT NULL AUTO_INCREMENT,
		operator VARCHAR(80) NOT NULL DEFAULT 'admin',
		role VARCHAR(20) NOT NULL DEFAULT 'user',
		content MEDIUMTEXT NULL,
		action_json JSON NULL,
		result_json JSON NULL,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (id),
		INDEX idx_operator_time (operator, create_time),
		INDEX idx_create_time (create_time)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	var n int
	err := a.db.QueryRow("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'ai_chat_message' AND column_name = 'session_id'").Scan(&n)
	if err == nil && n == 0 {
		if _, err := a.db.Exec("ALTER TABLE ai_chat_message ADD COLUMN session_id BIGINT NOT NULL DEFAULT 0, ADD INDEX idx_session_time (session_id, create_time)"); err != nil {
			log.Printf("[WARN] add session_id failed: %v", err)
		}
	}
}

func (a *app) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tpl.Execute(w, map[string]any{
		"db_host": env("DB_HOST", a.cfg.DBHost),
		"db_port": env("DB_PORT", a.cfg.DBPort),
		"db_name": env("DB_NAME", a.cfg.DBName),
	})
}

func (a *app) api(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/login" {
		a.login(w, r)
		return
	}
	if r.URL.Path == "/api/logout" {
		a.logout(w, r)
		return
	}
	user, ok := a.currentUser(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "未登录")
		return
	}

	switch {
	case r.URL.Path == "/api/get-bg-url":
		a.getBG(w, r)
	case r.URL.Path == "/api/summary":
		a.summary(w, r)
	case r.URL.Path == "/api/analytics":
		a.analytics(w, r)
	case r.URL.Path == "/api/logs":
		a.logs(w, r)
	case r.URL.Path == "/api/log-operators":
		a.logOperators(w, r)
	case r.URL.Path == "/api/users":
		a.users(w, r, user)
	case strings.HasPrefix(r.URL.Path, "/api/users/"):
		a.userByID(w, r, user)
	case r.URL.Path == "/api/dynamics":
		a.dynamics(w, r, user)
	case strings.HasPrefix(r.URL.Path, "/api/dynamics/"):
		a.dynamicByID(w, r, user)
	case r.URL.Path == "/api/friend-applies":
		a.friendApplies(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/friend-applies/"):
		a.friendApplyByID(w, r, user)
	case r.URL.Path == "/api/friends":
		a.friends(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/friends/"):
		a.friendByIDs(w, r, user)
	case r.URL.Path == "/api/star-notices":
		a.starNotices(w, r, user)
	case r.URL.Path == "/api/admin-notices":
		a.adminNotices(w, r, user)
	case r.URL.Path == "/api/ai/chat":
		a.aiChat(w, r, user)
	case r.URL.Path == "/api/ai/sessions":
		a.aiSessions(w, r, user)
	case strings.HasPrefix(r.URL.Path, "/api/ai/sessions/"):
		a.aiSessionByID(w, r, user)
	case strings.HasPrefix(r.URL.Path, "/api/admin-notices/"):
		a.adminNoticeByID(w, r, user)
	default:
		writeErr(w, http.StatusNotFound, "接口不存在")
	}
}
