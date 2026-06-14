# 跨机调用:Cloudflare Tunnel + Access(方案 A)

> 让 **kungal-neo(新,Dokploy 容器里的论坛)** 跨机调到 **kungal-old(旧机,link-checker 跑 `127.0.0.1:6734`)** 上的服务,且 **kungal-old 不开任何公网端口**、只有论坛能调。
>
> 本服务端口 **6734**,对外 hostname **`link-checker-kungal.nextmoe.dev`**(`nextmoe.dev` 需是你 Cloudflare 账号下的 zone)。

## 0. 为什么是这套

```
 kungal-old(旧机)                         Cloudflare 边缘            kungal-neo(Dokploy)
 ┌───────────────────────┐                ┌──────────────┐         ┌────────────────────┐
 │ link-checker          │  出站长连(无    │  Access 网关  │  HTTPS  │ 论坛容器           │
 │   127.0.0.1:6734  ◄───┼── 入站端口)─────│ 校验 service  │◄────────┤ outbound 调用      │
 │ cloudflared ──────────┼───────────────►│  token       │         │ link-checker-      │
 └───────────────────────┘                └──────────────┘         │ kungal.nextmoe.dev │
                                                                     └────────────────────┘
```

- `cloudflared` 在 old 上**只往 Cloudflare 边缘建出站连接**,old **不暴露任何入站端口**、无需防火墙规则。
- 论坛容器只是发一个**普通 outbound HTTPS**(容器访问公网天生没问题,neo 侧零网络改动)。
- **两层鉴权**:① Cloudflare Access 用 **service token** 把来源锁死成"只有论坛"(无 token 直接被边缘 403,根本到不了 old);② 服务自身的 `Authorization: Bearer <LLC key>`。

## 1. 前置

- `link-checker` 已在 kungal-old 上跑通,绑 `127.0.0.1:6734`(见 [DEPLOY.md](./DEPLOY.md) 的 PM2 节;`/opt/link-checker/.env` 里 `LLC_ADDR=127.0.0.1:6734`)。
- `nextmoe.dev` 已托管在你的 Cloudflare 账号(Zone)。
- 一个能开 Zero Trust 面板的 Cloudflare 账号(免费版即可)。

## 2. 在 kungal-old 上装 cloudflared

```bash
# Debian 12(bookworm)
sudo mkdir -p /usr/share/keyrings
curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg \
  | sudo tee /usr/share/keyrings/cloudflare-main.gpg >/dev/null
echo "deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared bookworm main" \
  | sudo tee /etc/apt/sources.list.d/cloudflared.list
sudo apt-get update && sudo apt-get install -y cloudflared
cloudflared --version
```

## 3. 登录 + 建 tunnel + DNS 路由

```bash
# 浏览器授权(选 nextmoe.dev 这个 zone),cert 写到 /root/.cloudflared/cert.pem
sudo cloudflared tunnel login

# 建一条具名 tunnel,生成凭据 JSON /root/.cloudflared/<TUNNEL_ID>.json
sudo cloudflared tunnel create link-checker

# 把 hostname 指到这条 tunnel(自动写一条 CNAME 到 Cloudflare DNS)
sudo cloudflared tunnel route dns link-checker link-checker-kungal.nextmoe.dev
```

记下 `cloudflared tunnel create` 打印的 **TUNNEL_ID**。

## 4. ingress 配置

`/etc/cloudflared/config.yml`:

```yaml
tunnel: <TUNNEL_ID>
credentials-file: /root/.cloudflared/<TUNNEL_ID>.json

# 收到 link-checker-kungal.nextmoe.dev 的请求 → 回源到本机 6734;其余一律 404
ingress:
  - hostname: link-checker-kungal.nextmoe.dev
    service: http://127.0.0.1:6734
  - service: http_status:404
```

```bash
sudo cloudflared tunnel ingress validate    # 校验配置
```

## 5. 让 cloudflared 常驻(开机自启)

`cloudflared` 是基础设施进程,用它自带的 systemd 安装最稳(不依赖 kun 的 Node/PM2):

```bash
sudo cloudflared service install     # 读 /etc/cloudflared/config.yml 生成 systemd 服务
sudo systemctl enable --now cloudflared
systemctl status cloudflared
journalctl -u cloudflared -f         # 看连上 Cloudflare 边缘的日志
```

> 想统一进 PM2 也行(用 kun):`pm2 start cloudflared --name cf-link-checker -- tunnel run link-checker && pm2 save`。但 systemd 更适合这种系统级 daemon。

此刻 `https://link-checker-kungal.nextmoe.dev/healthz` 已经能通——**但还没锁来源,下一步必须做 Access**,否则等于公网裸奔(只剩 API key 一层)。

## 6. Cloudflare Access:把来源锁成"只有论坛"

> Zero Trust 面板(one.dash.cloudflare.com)已改版到 **Access controls** 一级目录,以下为当前(2026-06)路径。

**6.1 建 service token**(给论坛这台非交互式调用方)
- **Zero Trust → Access controls → Service credentials → Service Tokens → Create Service Token**
- 命名 `forum-link-checker`,选 **Service Token Duration**(有效期;到期可在面板 **Refresh** 续 1 年),点 **Generate token**
- 生成页**当场复制** `Client ID`(形如 `xxxxx.access`)和 `Client Secret`(连同对应的 header 名;**只显示这一次**,丢了只能重建)

**6.2 建 Access Application 保护这个 hostname**
- **Zero Trust → Access controls → Applications → Create new application → Self-hosted and private → Add public hostname**
- **Domain** 下拉选 `nextmoe.dev`,子域填 `link-checker-kungal`(或点 **Switch to custom input** 直接填 `link-checker-kungal.nextmoe.dev`)
- **Policy**(可"add existing policy"或"create a new policy";Access **默认 deny**,只放行命中 include 的):
  - **Action = Service Auth**(非交互式,只认 token,**不弹 IdP 登录页**)
  - **Include** → Selector **Service Token** → 选 `forum-link-checker`
- **Additional settings** 标签:打开 **「401 Response for Service Auth policies」** —— 没带(或带错)token 的请求直接回 **401**,而不是跳 HTML 登录页;对 API 调用方更干净。
- 保存。

> 这样:**没带 service token 的请求在 Cloudflare 边缘就被挡掉(401)**,根本到不了 old 上的 cloudflared/服务。reusable policy 也可在 **Access controls → Policies** 单独管理。

## 7. 论坛侧(kungal-neo,等接闸门时)

容器里发**普通 HTTPS**,带 CF service token 两个头 + 你的 LLC key:

```bash
curl -sS https://link-checker-kungal.nextmoe.dev/v1/check \
  -H "CF-Access-Client-Id: <CLIENT_ID>.access" \
  -H "CF-Access-Client-Secret: <CLIENT_SECRET>" \
  -H "Authorization: Bearer <LLC_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://pan.quark.cn/s/<pwd_id>","passcode":""}'
# => {"provider":"quark","status":"alive|dead|unknown","reason":"...","cached":false}
```

论坛配置(放 Dokploy Environment 面板,绝不入库):

```
LINK_CHECKER_BASE_URL=https://link-checker-kungal.nextmoe.dev
LINK_CHECKER_API_KEY=<LLC_API_KEY>
CF_ACCESS_CLIENT_ID=<CLIENT_ID>.access
CF_ACCESS_CLIENT_SECRET=<CLIENT_SECRET>
```

> 调用约定:`POST {BASE}/v1/check`,超时设紧(如 8s);**任何失败/超时/非 2xx 一律当 `unknown`**,回退人工,绝不让 checker 抖动拖垮报告流程。

## 8. 验证(三连)

```bash
# ① 带齐 CF token + 正确 LLC key → 200 + 判定
curl -s https://link-checker-kungal.nextmoe.dev/v1/check \
  -H "CF-Access-Client-Id: <ID>.access" -H "CF-Access-Client-Secret: <SECRET>" \
  -H "Authorization: Bearer <LLC_API_KEY>" \
  -d '{"url":"https://pan.quark.cn/s/eb34b875e97f"}'        # 期望 alive

# ② 不带 CF token → 被 Cloudflare 边缘挡掉(开了「401 Response」设置则回 401)
curl -s -i https://link-checker-kungal.nextmoe.dev/healthz | head -1   # 期望 401(若没开该设置则是 302→登录页)
#    关键:body 不是本服务的 {"status":"ok"} / {"error":...},而是 Cloudflare 的拒绝响应 → 证明根本没进到服务

# ③ 带 CF token 但 LLC key 错 → 进到服务、由服务回 401(body 是本服务 JSON)
curl -s https://link-checker-kungal.nextmoe.dev/v1/check \
  -H "CF-Access-Client-Id: <ID>.access" -H "CF-Access-Client-Secret: <SECRET>" \
  -H "Authorization: Bearer wrong" -d '{"url":"x"}'         # 期望 {"error":"unauthorized"}(本服务返回)
```

> ②③ 都可能是 401,**靠 body 区分**:② 是 Cloudflare 边缘的拒绝(没进服务)、③ 是本服务的 `{"error":"unauthorized"}`(进了服务、key 不对)。

## 9. 要点与排错

- **零入站端口**:old 上 `ss -ltnp` 看不到 cloudflared 的监听口——它只出站。link-checker 仍只绑 `127.0.0.1:6734`,**不要**改成 `0.0.0.0`。
- **本地健康检查不受影响**:PM2/systemd 的 `/app healthcheck` 走 `127.0.0.1:6734` 直连,不经 Cloudflare。Cloudflare 侧若想探活,用带 token 的 `/healthz`。
- **轮换**:service token 与 LLC key 都可在面板/`.env` 里轮换;LLC key 改完 `pm2 reload link-checker`(别加 `--update-env`)。
- **502/不通**:查 `journalctl -u cloudflared -f`(隧道是否 connected)、`curl 127.0.0.1:6734/healthz`(本地服务是否活)、Access Application 的 hostname/policy 是否对。
- **不要叠** 公网防火墙开放 6734 —— 全程只走 cloudflared 出站,6734 永远只在 old 本机回环。

## 10. 拆除

```bash
sudo systemctl disable --now cloudflared
sudo cloudflared tunnel delete link-checker      # 删 tunnel
# 面板删掉 Access Application + Service Token + DNS 记录
```
