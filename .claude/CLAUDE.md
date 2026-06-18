# Fool Chat Admin Go

## 部署

```bash
# 编译 Linux 二进制
GOOS=linux GOARCH=amd64 go build -o fool_chat_admin_go .

# 上传到服务器
scp ./fool_chat_admin_go root@39.104.80.114:/opt/fool_chat_admin_go/fool_chat_admin_go

# 重启服务
ssh root@39.104.80.114 "systemctl restart fool_chat_admin_go"
```

## 本地开发

```bash
go run .
```

默认监听 `0.0.0.0:9100`，配置在 `config.yaml`。
