# 部署指南

## 1. 前置条件

| 组件 | 版本 | 说明 |
|---|---|---|
| Go | 1.26+ | 仅构建阶段需要 |
| MySQL | 8.0 | `utf8mb4` / `utf8mb4_unicode_ci` |
| 系统 | Linux x86_64 | systemd 托管，或任意能跑二进制的方式 |
| 反向代理 | Nginx 1.20+ | 终止 TLS 并转发到本地端口 |

单机 2 核 4 G 足够起步；瓶颈通常在数据库连接与上传目录磁盘。

## 2. 构建

```bash
git clone https://github.com/acebluewgf/geo-content-hub.git
cd geo-content-hub
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o contenthub ./cmd/contenthub
```

前端构建产物已提交在 `internal/server/dist`，构建时会自动嵌入。改过前端则先 `cd web && npm ci && npm run build`。

## 3. 目录与配置

```bash
sudo mkdir -p /opt/contenthub /opt/contenthub/storage/uploads
sudo useradd -r -s /usr/sbin/nologin contenthub
sudo chown -R contenthub:contenthub /opt/contenthub
sudo cp contenthub /opt/contenthub/
```

`/opt/contenthub/.env.local`：

```dotenv
APP_ENV=production
APP_ADDR=127.0.0.1:8080
APP_BASE_URL=https://your-domain.example
DB_HOST=127.0.0.1
DB_PORT=3306
DB_NAME=contenthub
DB_USER=contenthub
DB_PASSWORD=<强密码>
MEDIA_DIR=/opt/contenthub/storage/uploads
SESSION_SECRET=<openssl rand -hex 32>
```

```bash
sudo chown contenthub:contenthub /opt/contenthub/.env.local
sudo chmod 600 /opt/contenthub/.env.local
```

`APP_BASE_URL` 必须与对外访问地址一致（含 https），否则图片与回跳链接会指向错误域名。

## 4. 数据库

```sql
CREATE DATABASE contenthub DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'contenthub'@'127.0.0.1' IDENTIFIED BY '<强密码>';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, INDEX, REFERENCES ON contenthub.* TO 'contenthub'@'127.0.0.1';
FLUSH PRIVILEGES;
```

不建议用 root；迁移需要 DDL 权限，稳定后可再回收 `CREATE/ALTER/INDEX`。

```bash
sudo -u contenthub /opt/contenthub/contenthub db:check     # 版本/字符集/时区
sudo -u contenthub /opt/contenthub/contenthub migrate
sudo -u contenthub /opt/contenthub/contenthub user:create --username admin --role admin
```

## 5. systemd

`/etc/systemd/system/contenthub.service`：

```ini
[Unit]
Description=GEO Content Hub
After=network-online.target mysql.service
Wants=network-online.target

[Service]
Type=simple
User=contenthub
Group=contenthub
WorkingDirectory=/opt/contenthub
ExecStart=/opt/contenthub/contenthub serve
Restart=always
RestartSec=3
Environment=TZ=Asia/Shanghai
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=/opt/contenthub/storage
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now contenthub
sudo systemctl status contenthub
journalctl -u contenthub -f
```

## 6. Nginx

```nginx
server {
    listen 443 ssl http2;
    server_name your-domain.example;

    ssl_certificate     /etc/letsencrypt/live/your-domain.example/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/your-domain.example/privkey.pem;

    client_max_body_size 20m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 120s;
    }
}

server {
    listen 80;
    server_name your-domain.example;
    return 301 https://$host$request_uri;
}
```

健康检查：`GET /healthz` 返回 `{"db":true,"scheduler":true,"version":"x.y.z"}`。

## 7. 升级与回滚

```bash
# 升级
sudo systemctl stop contenthub
sudo cp /opt/contenthub/contenthub /opt/contenthub/contenthub.bak
sudo cp contenthub /opt/contenthub/
sudo -u contenthub /opt/contenthub/contenthub migrate
sudo systemctl start contenthub

# 回滚（迁移不可逆，回滚前先确认没有破坏性变更）
sudo systemctl stop contenthub
sudo cp /opt/contenthub/contenthub.bak /opt/contenthub/contenthub
sudo systemctl start contenthub
```

## 8. 备份

至少三样：数据库、`storage/uploads`、`.env.local`（换机时用于恢复密钥）。

```bash
# 每日 3:10 备份，保留 14 天
10 3 * * * mysqldump --single-transaction --routines contenthub | gzip > /backup/contenthub-$(date +\%F).sql.gz
20 3 * * * tar czf /backup/uploads-$(date +\%F).tar.gz -C /opt/contenthub storage/uploads
15 3 * * * find /backup -name "contenthub-*.sql.gz" -mtime +14 -delete
```

## 9. 上线检查清单

- [ ] `db:check` 三项（版本 / 字符集 / 时区）均通过
- [ ] `migrate` 无待应用脚本
- [ ] `APP_BASE_URL` 为 https 且与访问地址一致
- [ ] `.env.local` 权限 600，属主为运行用户
- [ ] `/healthz` 中 `db` 与 `scheduler` 均为 true
- [ ] 上传图片可访问、可删除
- [ ] 备份任务已生效并验证过一次恢复