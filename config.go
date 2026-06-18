package main

import (
	"os"
	"strings"
)

type appConfig struct {
	AppAddr        string
	DBHost         string
	DBPort         string
	DBName         string
	DBUser         string
	DBPassword     string
	SessionKey     string
	DeletePassword string
	AIBaseURL      string
	AIAPIKey       string
	AIModel        string
}

func loadConfig(path string) appConfig {
	cfg := appConfig{
		AppAddr:        "127.0.0.1:9100",
		DBHost:         "39.104.80.114",
		DBPort:         "3308",
		DBName:         "mhkh",
		DBUser:         "root",
		DBPassword:     "123456",
		SessionKey:     "fool-chat-admin",
		DeletePassword: "zzt",
		AIBaseURL:      "https://api.deepseek.com",
		AIModel:        "deepseek-chat",
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
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
