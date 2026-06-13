# kungal-link-live-checker

> 网盘分享链接「存活校验」服务 —— 给定一个网盘分享链接(可带提取码),用各网盘自家的分享状态 API 客观判断它是 **有效 / 已失效 / 无法判定**。

一个**独立的、可被多个项目复用**的小服务。第一个消费方是 [kun-galgame-forum](https://github.com/KunMoe/kun-galgame-forum)(论坛的「资源报告失效」功能),未来可被 kun-galgame-patch(moyu / 补丁站)等复用。

状态:🚧 **Phase 0 + 1 + 2 已落地(夸克 / UC / 百度,≈ 71% 链接)**。骨架(三态核心、HTTP API、s2s 鉴权、缓存、按 provider 限流、配置)+ 三家 checker 已端到端跑通,并对**真实生产链接**(含已失效分享)逐条验证 `alive`/`dead`/`unknown` 判定。详见 [§ 路线图](#路线图)。

> 🤖 **接手本项目的 AI 代理**:先读 [`CLAUDE.md`](CLAUDE.md)(它会被 Claude Code 自动加载),再读 `README.md` → `docs/REQUIREMENTS.md` → `docs/PROVIDERS.md`。

---

## 为什么有这个项目

下游站点(论坛 / 补丁站)的 Galgame 资源大量是网盘分享链接。现在判断一个链接「是否失效」靠**用户主观报告**:一个人点一下「报告失效」就立刻把资源标记为失效——网络抽风、打不开等误判会造成大量**误报**,只能由发布者手动恢复,苦不堪言。

**根因**:这些链接无法用"裸 HTTP 探测"判断死活——网盘有反爬 + 登录墙,文件删了页面照样 200;磁链根本没有 HTTP。

**本项目的思路**:不抓 HTML,而是**调各网盘自家的「分享状态」JSON API**。这些 API 对已删除/失效的分享会返回**确定性的错误码**——拿到这种码,就是 ~100% 确定失效。配合保守策略(见下),把"机器能 100% 确定"的那部分从人工猜测里彻底解放出来。

### 这值得做吗?数据说话

对论坛当前 **23,865 条**资源链接按域名分类(2026-06,见 [docs/REQUIREMENTS.md](docs/REQUIREMENTS.md#资源链接分布实测)):

| 网盘 | 占比 | 可客观校验 |
|---|---|---|
| 百度网盘 | 35.6% | ✅(最难,反爬凶) |
| 夸克网盘 | 28.8% | ✅(已验证,API 干净) |
| UC 网盘 | 7.0% | ✅(夸克同源) |
| 和彩云 | 6.6% | ✅ |
| 迅雷网盘 | 3.8% | ✅ |
| 123 盘 | 1.4% | ✅ |
| 磁链 / onedrive / mega / 友站 / 图床 … | ~16% | ❌(尾巴,人工兜底) |

**前 5 家网盘 ≈ 81.8% 的链接可客观校验。** 先吃下这 80%,剩下 ~16% 的尾巴回退到下游的人工机制。

---

## 核心契约:保守的三态判定

服务对每个链接只回三种状态。**这套三态是正确性的核心**:

| status | 含义 | 触发条件 | 下游应如何处理 |
|---|---|---|---|
| `alive` | 确定有效 | 网盘 API 明确返回"分享存在且可访问"(如夸克 `code:0`) | 可据此**驳回**误报("经核验仍可访问") |
| `dead` | 确定失效 | 网盘 API 明确返回"分享不存在 / 已删除 / 已取消 / 已失效 / 已屏蔽" | **唯一**可据此自动标记失效的状态 |
| `unknown` | 无法判定 | 其余一切:缺提取码、限流、验证码、超时、网络错误、不支持的网盘、**看不懂的响应** | **绝不自动判失效**,回退人工机制 |

铁律:**任何"看不懂 / 不确定"的情况一律归 `unknown`,永不归 `dead`。** 这样即使网盘改了 API(必然会发生),最坏只是退化成人工兜底,绝不会误杀一条好链接。详见 [docs/REQUIREMENTS.md § 保守策略](docs/REQUIREMENTS.md#保守策略不确定就是-unknown)。

---

## 它如何被使用(报告闸门)

下游(如论坛)把它接成「用户报告失效」的**闸门**,而不是让一次主观点击直接生效:

```
用户报告某资源失效
        │
        ▼
  POST /v1/check { url, passcode }
        │
   ┌────┼─────────────────────────┐
   ▼    ▼                          ▼
 dead  alive                    unknown
   │    │                          │
   ▼    ▼                          ▼
立即判失效  当场驳回报告        回退人工机制
(机器证实) (链接仍可访问)     (法定人数/通知发布者)
```

对那 ~80% 可校验链接:**误报被当场驳回、真失效被机器证实**——误判清零、发布者零负担。剩下的尾巴才落到人工。

---

## 架构概览

- **形态**:独立 HTTP 微服务(Go),对外一个简单的 `POST /v1/check`(+ 批量端点)。s2s 鉴权(API Key / Basic)。
- **provider 插件化**:每家网盘一个 `Checker` 实现,统一接口 `Check(ctx, url, passcode) → Verdict`。核心不掺任何下游业务。
- **缓存**:`dead` 结果长缓存、`alive` 中等 TTL、`unknown` 极短/不缓存。多消费方共享缓存(论坛查过的链接,补丁站直接命中)。
- **限流 / 退避**:按 provider 维度的全局限流预算,防止把出口 IP 打进网盘黑名单。
- **按需为主**:初期只在"被报告时"校验,**不做全量爬取**(全量必被封)。后台低频复检是后话。
- **部署隔离**:执行抓取的出口 IP **必须**与 OAuth/身份服务的 IP 分开——网盘封 IP 不得波及登录。这也是它不放进 kun-galgame-infra 的根本原因(爬虫不碰信任根)。

详见 [docs/REQUIREMENTS.md](docs/REQUIREMENTS.md)。各网盘 API 的逆向细节与状态码映射见 [docs/PROVIDERS.md](docs/PROVIDERS.md)。

---

## 路线图

- **Phase 0 — 骨架** ✅:核心接口、HTTP API、s2s 鉴权、缓存、按 provider 限流、配置。
- **Phase 1 — 夸克 + UC**(~36%)✅:两家均已对真实 API 验证(夸克 `0/41004/41006/41011/41012/41010/41031`、UC 同源 `pc-api.uc.cn`)。
- **Phase 2 — 百度**(35.6%)✅:走 `shorturlinfo`(只需 `BAIDUID` cookie,无需登录),实测验证 `errno -21 → dead`、`0/-9 → alive`。三家合计 ≈ **71%** 链接可客观校验。下一步:接成论坛报告闸门(待论坛就绪)。
- **Phase 3 — 和彩云 / 迅雷 / 123 盘**(端点已检索,见 docs/PROVIDERS.md)。
- **以后**:后台低频复检、代理/IP 池、更多 provider、尾巴(磁链/友站/图床)的兜底策略。

每一档都可独立交付、随时停在某档。

---

## 开发

Go service,**零外部依赖、单二进制**(stdlib only)。

```bash
go test ./...                              # 全部测试(含「永不误判 dead」的铁律守卫测试)
go build -o bin/llc ./cmd/server           # 构建单二进制

# 运行(最少只需一把 API key;配置见 .env.example)
LLC_API_KEYS=$(openssl rand -hex 16) go run ./cmd/server
```

调用(s2s,Bearer 鉴权):

```bash
curl -s -X POST localhost:8080/v1/check \
  -H "Authorization: Bearer $LLC_API_KEYS" \
  -d '{"url":"https://pan.quark.cn/s/<pwd_id>","passcode":"<可选提取码>"}'
# => {"provider":"quark","status":"alive|dead|unknown","reason":"...","providerCode":"...","cached":false}
```

完整配置项见 [`.env.example`](.env.example)。代码结构:`cmd/server` 入口;`internal/checker` 三态核心与 provider 注册表;`internal/provider/*` 各网盘 checker;`internal/{cache,ratelimit,config,httpapi,service}` 地基。

## 名字

`kungal-link-live-checker` —— kungal 生态里专做**网盘分享链接存活校验**(link **live**-ness checker)的服务,不是通用死链/SEO 爬虫。

## 许可

License 待定(由仓库所有者决定,建议 MIT / AGPL 之一)。
