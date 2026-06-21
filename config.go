package main

import (
	"bytes"
	"log"
	"os"
	"strings"
)

type appConfig struct {
	AppAddr         string
	DBHost          string
	DBPort          string
	DBName          string
	DBUser          string
	DBPassword      string
	SessionKey      string
	DeletePassword  string
	AIBaseURL       string
	AIAPIKey        string
	AIModel         string
	RedisHost       string
	RedisPort       string
	RedisPassword   string
	VerifyAddr      string
	GateAddr        string
	StatusAddr      string
	ChatServer1Addr string
	ChatServer2Addr string
}

func loadConfig(path string) appConfig {
	cfg := appConfig{
		AppAddr:         "0.0.0.0:9100",
		DBHost:          "127.0.0.1",
		DBPort:          "3306",
		DBName:          "mhkh",
		DBUser:          "root",
		DBPassword:      "",
		SessionKey:      "fool-chat-admin",
		DeletePassword:  "",
		AIBaseURL:       "https://api.deepseek.com",
		AIAPIKey:        "",
		AIModel:         "deepseek-chat",
		RedisHost:       "127.0.0.1",
		RedisPort:       "6379",
		RedisPassword:   "",
		VerifyAddr:      "127.0.0.1:50051",
		GateAddr:        "127.0.0.1:8080",
		StatusAddr:      "127.0.0.1:50052",
		ChatServer1Addr: "127.0.0.1:8090",
		ChatServer2Addr: "127.0.0.1:8091",
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[WARN] config file %s not loaded: %v", path, err)
		return cfg
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	section := ""
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, ":") {
			section = strings.TrimSuffix(line, ":")
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := trim(strings.TrimSpace(parts[1]), 0)
		value = strings.Trim(value, `"'`)
		switch section + "." + key {
		case "app.addr":
			cfg.AppAddr = value
		case "database.host":
			cfg.DBHost = value
		case "database.port":
			cfg.DBPort = value
		case "database.name":
			cfg.DBName = value
		case "database.user":
			cfg.DBUser = value
		case "database.password":
			cfg.DBPassword = value
		case "security.session_key":
			cfg.SessionKey = value
		case "security.delete_password":
			cfg.DeletePassword = value
		case "ai.base_url":
			cfg.AIBaseURL = value
		case "ai.api_key":
			cfg.AIAPIKey = value
		case "ai.model":
			cfg.AIModel = value
		case "redis.host":
			cfg.RedisHost = value
		case "redis.port":
			cfg.RedisPort = value
		case "redis.password":
			cfg.RedisPassword = value
		case "verify_server.addr":
			cfg.VerifyAddr = value
		case "services.gate_server":
			cfg.GateAddr = value
		case "services.status_server":
			cfg.StatusAddr = value
		case "services.chat_server1":
			cfg.ChatServer1Addr = value
		case "services.chat_server2":
			cfg.ChatServer2Addr = value
		}
	}
	return cfg
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
