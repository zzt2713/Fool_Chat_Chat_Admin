package main

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type monitorItem struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Addr        string `json:"addr"`
	Online      bool   `json:"online"`
	LatencyMS   int64  `json:"latency_ms"`
	OnlineUsers int64  `json:"online_users,omitempty"`
	Error       string `json:"error,omitempty"`
}

type monitorResp struct {
	GeneratedAt string        `json:"generated_at"`
	TotalOnline int64         `json:"total_online"`
	Services    []monitorItem `json:"services"`
	Depends     []monitorItem `json:"depends"`
}

func (a *app) serviceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, "方法不支持")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	redisClient := a.redisClient()
	defer redisClient.Close()

	depends := []monitorItem{
		a.checkMySQL(ctx),
		a.checkRedis(ctx, redisClient),
	}

	services := []monitorItem{
		checkTCPService("GateServer", "HTTP 网关", env("GATE_SERVER_ADDR", a.cfg.GateAddr)),
		checkTCPService("StatusServer", "gRPC 状态服务", env("STATUS_SERVER_ADDR", a.cfg.StatusAddr)),
		checkTCPService("VerifyServer", "gRPC 验证码服务", env("VERIFY_SERVER_ADDR", a.cfg.VerifyAddr)),
		a.checkChatService(ctx, redisClient, "ChatServer1", env("CHAT_SERVER1_ADDR", a.cfg.ChatServer1Addr), "chatserver1"),
		a.checkChatService(ctx, redisClient, "ChatServer2", env("CHAT_SERVER2_ADDR", a.cfg.ChatServer2Addr), "chatserver2"),
	}

	var total int64
	for _, s := range services {
		total += s.OnlineUsers
	}

	writeJSON(w, monitorResp{
		GeneratedAt: time.Now().Format(time.RFC3339),
		TotalOnline: total,
		Services:    services,
		Depends:     depends,
	})
}

func (a *app) checkMySQL(ctx context.Context) monitorItem {
	start := time.Now()
	item := monitorItem{Name: "MySQL", Kind: "数据库", Addr: env("DB_HOST", a.cfg.DBHost) + ":" + env("DB_PORT", a.cfg.DBPort)}
	if err := a.db.PingContext(ctx); err != nil {
		item.Error = err.Error()
		item.LatencyMS = time.Since(start).Milliseconds()
		return item
	}
	item.Online = true
	item.LatencyMS = time.Since(start).Milliseconds()
	return item
}

func (a *app) checkRedis(ctx context.Context, client *redis.Client) monitorItem {
	start := time.Now()
	item := monitorItem{Name: "Redis", Kind: "缓存 / 登录态", Addr: env("REDIS_ADDR", a.cfg.RedisHost+":"+a.cfg.RedisPort)}
	if err := client.Ping(ctx).Err(); err != nil {
		item.Error = err.Error()
		item.LatencyMS = time.Since(start).Milliseconds()
		return item
	}
	item.Online = true
	item.LatencyMS = time.Since(start).Milliseconds()
	return item
}

func (a *app) checkChatService(ctx context.Context, client *redis.Client, name, addr, redisKey string) monitorItem {
	item := checkTCPService(name, "TCP 聊天服务", addr)
	count, err := client.HGet(ctx, "logincount", redisKey).Result()
	if err != nil && err != redis.Nil {
		if item.Error != "" {
			item.Error += "；Redis 在线人数读取失败：" + err.Error()
		} else {
			item.Error = "Redis 在线人数读取失败：" + err.Error()
		}
		return item
	}
	if err == redis.Nil || count == "" {
		return item
	}
	if n, parseErr := strconv.ParseInt(count, 10, 64); parseErr == nil {
		item.OnlineUsers = n
	} else {
		item.Error = "在线人数格式异常：" + count
	}
	return item
}

func checkTCPService(name, kind, addr string) monitorItem {
	start := time.Now()
	item := monitorItem{Name: name, Kind: kind, Addr: addr}
	if addr == "" {
		item.Error = "未配置地址"
		return item
	}
	conn, err := net.DialTimeout("tcp", addr, 1200*time.Millisecond)
	item.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		item.Error = err.Error()
		return item
	}
	_ = conn.Close()
	item.Online = true
	return item
}
