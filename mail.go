package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"strings"
)

func (a *app) sendSMTP(to, subject, body string) error {
	host := a.cfg.SMTPHost
	port := a.cfg.SMTPPort
	user := a.cfg.SMTPUser
	pass := a.cfg.SMTPPassword
	from := a.cfg.SMTPFrom
	if from == "" {
		from = user
	}
	if host == "" || port == "" || user == "" || pass == "" {
		return fmt.Errorf("SMTP 未配置完整")
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		from, to, subject, body)

	addr := net.JoinHostPort(host, port)
	auth := smtp.PlainAuth("", user, pass, host)

	if port == "465" {
		return a.sendSMTPS(addr, auth, from, to, msg)
	}
	return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
}

func (a *app) sendSMTPS(addr string, auth smtp.Auth, from, to, msg string) error {
	host, _, _ := net.SplitHostPort(addr)

	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("TLS dial: %w", err)
	}
	defer conn.Close()

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	if err = c.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err = c.Mail(from); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err = c.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err = w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	return w.Close()
}

func (a *app) generateAIReply(ctx context.Context, account, email, reason string) string {
	if a.cfg.AIAPIKey == "" {
		return fmt.Sprintf("亲爱的 %s，感谢你对 Fool Chat 管理团队的关注。经过认真评估，我们暂时无法批准你的管理员申请。%s如有疑问，请联系超级管理员。祝好！", account, reason)
	}

	baseURL := strings.TrimRight(a.cfg.AIBaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	chatURL := baseURL + "/v1/chat/completions"
	if strings.HasSuffix(baseURL, "/v1") {
		chatURL = baseURL + "/chat/completions"
	}
	model := a.cfg.AIModel
	if model == "" {
		model = "deepseek-chat"
	}

	prompt := fmt.Sprintf(`你是一个友善的客服助手。请根据以下信息生成一封管理员申请拒绝通知邮件（纯文本，不要 HTML）：

申请人：%s
拒绝原因：%s

要求：
1. 语气友善、温和
2. 先肯定申请人的热情
3. 委婉说明拒绝原因
4. 鼓励后续继续申请
5. 结尾祝福
6. 200字以内`, account, reason)

	reqBody := openAIChatRequest{
		Model: model,
		Messages: []openAIMessage{
			{Role: "system", Content: "你是一个友善的客服助手，擅长撰写温和的拒绝通知。"},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.7,
	}
	data, _ := json.Marshal(reqBody)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", chatURL, bytes.NewReader(data))
	if err != nil {
		return ""
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.cfg.AIAPIKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		log.Printf("[WARN] AI reply generate failed: %v", err)
		return ""
	}
	defer resp.Body.Close()

	var chatResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return ""
	}
	if len(chatResp.Choices) > 0 {
		return chatResp.Choices[0].Message.Content
	}
	return ""
}
