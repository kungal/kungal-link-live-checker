# 部署:CI/CD(GHCR + GitHub Actions + Dokploy)

> 对齐 `kun-galgame-infra/docs/deploy`(尤其 [13-registry-ci.md](../../kun-galgame-infra/docs/deploy/13-registry-ci.md) 与 [12-dokploy.md](../../kun-galgame-infra/docs/deploy/12-dokploy.md))。**生产机零构建**:CI 在别处把镜像 build 好推 GHCR,Dokploy 只拉镜像。

## 流水线

```
push main ─► GitHub Actions(.github/workflows/build.yml)
              ├─ test : gofmt + go vet + go test -race(铁律守卫测试在此把关)
              ├─ build: 构建单二进制镜像 → 推 GHCR
              │         ghcr.io/kungal/kungal-link-live-checker:{latest, <git-sha>}
              └─ deploy: curl Dokploy webhook(未设 secret 时优雅跳过)
                          │
                          ▼
                  Dokploy ─► 拉 :latest ─► 滚动重部署(docker-compose.prod.yml)
```

- **镜像**:`Dockerfile` 多阶段——`golang:1.25` 静态编译(`CGO_ENABLED=0`)→ `distroless/static-debian13:nonroot`(~2MB、无 shell、nonroot,自带 ca-certificates 供出网 HTTPS)。**零外部依赖**,故无 `go.sum`。
- **两个 tag**:`:latest`(Dokploy 监听)+ `:<git-sha>`(不可变,回滚锚点)。回滚 = Dokploy 镜像引用临时改成某个已知良好的 `:<git-sha>` 再 redeploy。
- **健康检查**:distroless 无 curl,二进制自带 `healthcheck` 子命令(`/app healthcheck` 探自身 `/healthz`,据 `LLC_ADDR` 取端口)。

## 一次性配置(对齐 13-registry-ci §13.4 的坑)

1. **GHCR 包设公开**(或 Dokploy 加 `read:packages` PAT)→ 免凭证拉。
2. **Dokploy 建一个 Compose 应用**指向 `docker-compose.prod.yml`。
3. **设 secret**:把 Dokploy 应用的部署 Webhook URL 填进本仓 Actions secret **`DOKPLOY_WEBHOOK_LINKCHECKER`**。
4. **关掉 Dokploy 的 Auto Deploy**——否则 push 一到它就拉**上一次**的 `:latest`(构建还没好),和 CI 的晚触发赛跑。正确顺序是「构建完 → CI curl webhook → Dokploy 拉新镜像」。
5. **密钥** `LLC_API_KEYS` 填 Dokploy 各应用的 **Environment 面板**(逗号分隔,每个消费方一把);**绝不入库**。留空 = fail-closed,所有 `/v1` 返回 401。

## 网络与消费方

- 接 `dokploy-network`(external),服务名 `link-checker`(全局唯一)。**同机**消费方(论坛)直接 `http://link-checker:6734/v1/check`,带 `Authorization: Bearer <key>`。
- **不发布宿主端口、不默认开公网域名**(s2s,绝不匿名公开)。跨机消费方再按需用 Traefik 私有域名(compose 里有注释模板,鉴权仍靠服务自身的 API-key)。

## ⚠️ 出口 IP 隔离(架构铁律,REQUIREMENTS §7)

本服务会对网盘发**出站抓取**,出口 IP 可能被限流/封禁。**强烈建议跑在与 OAuth/身份(infra)分开的 Dokploy 服务器**上——封 IP 不得波及登录信任根。这正是它独立成仓、不并入 infra 的原因。同机部署能跑通,但共享出口 IP,有连带风险。

## 备用:无 Docker 主机(PM2 / systemd,独立出口机)

把 checker 放到一台**独立服务器**(出口 IP 与 OAuth/身份分离)时,无需 Docker——它是零依赖静态二进制。文件见 `deploy/`。

布局:`/opt/link-checker/{llc, .env, logs, ecosystem.config.js}`(全部 owner=运行用户)。

```bash
# 1) 上二进制(目标机没装 Go → 本地交叉编译再 scp)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o llc ./cmd/server
scp llc <host>:/opt/link-checker/llc

# 2) 配置 + 密钥(chmod 600,绝不入库;ecosystem 从这里读)
#    /opt/link-checker/.env:
#      LLC_ADDR=127.0.0.1:6734      # 私网/回环;切勿 0.0.0.0
#      LLC_API_KEYS=<openssl rand -hex 24>
#      LLC_RATE_RPS=5

# 3a) PM2(与其它 Node 服务统一管理)
cd /opt/link-checker && pm2 start ecosystem.config.js && pm2 save
#  更新:scp 新 llc → pm2 reload link-checker   (切勿加 --update-env:会丢掉 .env 里的 key)
# 3b) 或 systemd(原生,无需 Node):见 deploy/link-checker.service
```

调用方:**同机**消费方(论坛与 checker 同处一台)直接 `http://127.0.0.1:6734/v1/check` + `Authorization: Bearer <key>`;**跨机**(论坛在 Dokploy/kungal-neo、checker 在 kungal-old)用 **Cloudflare Tunnel + Access**——详见 [`cloudflare-tunnel.md`](./cloudflare-tunnel.md)(old 零入站端口、Access service token 锁来源 + API key 双层)。失败/超时一律当 `unknown`。

## 本地

```bash
docker compose up --build          # docker-compose.yml(dev,本地 build,API key=devkey)
# 或不走容器:
LLC_API_KEYS=$(openssl rand -hex 16) go run ./cmd/server
```
