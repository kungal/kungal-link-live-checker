# kungal-link-live-checker — 需求与设计文档

> 版本:草案 v0.1 · 2026-06-13
> 状态:设计阶段(尚未实现)

本文是 `kungal-link-live-checker` 的权威需求与设计说明。实现以本文为准;各网盘的逆向细节单独放在 [PROVIDERS.md](PROVIDERS.md)。

---

## 1. 背景与问题

下游站点(kun-galgame-forum 论坛、kun-galgame-patch 补丁站)的 Galgame 资源主要是**网盘分享链接**。下游需要知道一个链接是否还有效。

现状(以论坛为例):用户点「报告失效」→ 资源**立刻**被标记失效;无计数、无阈值、无去重。问题:

- **误报极易发生**:用户因网络、代理、地区限制等"自己打不开",就误报失效。
- **一次误报即生效**:单个主观信号直接改变资源状态。
- **只能发布者手动恢复**:负担全压在发布者身上,怨声载道。

为什么不能"直接 HTTP 探测链接死活":

- 网盘有**反爬 + 登录墙**:已删除的文件,分享页照样返回 `200`;
- 裸 `curl` 会被重定向到验证页(百度实测 `302`,body 135 字节)或拿到一个 SPA 空壳(夸克实测 `200`,真实状态在 JS 调的 JSON API 里);
- 磁力链(`magnet:`)根本没有 HTTP 端点可探。

**结论**:可靠性不能来自"猜",要来自**调各网盘自家的分享状态 API**,拿它返回的**确定性状态码**来判定。

---

## 2. 目标 / 非目标

### 目标
1. 给定 `(url, passcode?)`,对**支持的网盘**返回**保守的三态**:`alive` / `dead` / `unknown`(见 §3)。
2. 对主流网盘(百度/夸克/UC/和彩云/迅雷/123,合计 ~84% 的链接)做到:**只在网盘明确返回"分享不存在/已删除"时才判 `dead`**,准确率 ~100%。
3. 可被多个下游通过简单 HTTP API 复用,带共享缓存与按 provider 的限流。
4. 作为下游"报告失效"流程的**闸门**:`dead`→自动失效、`alive`→驳回误报、`unknown`→回退人工。

### 非目标(明确不做)
- ❌ **不是**通用死链/SEO 爬虫,不抓 HTML 找 `<a>`。
- ❌ **不**尝试对所有链接给出 `alive`(做不到——登录墙下 `200` 不代表文件还在)。本服务的价值在**确定性的 `dead`** 与**支持网盘的确定性 `alive`**。
- ❌ **不**下载、不解析文件内容,不做病毒/合规扫描。
- ❌ **不**破解提取码 / 验证码 / 登录,不撞库、不做凭证填充;只使用调用方提供的提取码。
- ❌ 初期**不**做全量后台爬取(全量必被封)。仅按需(被报告时)校验。
- ❌ **不**承载下游业务(失效计数、通知、阈值等)——那些留在下游。

---

## 3. 核心契约:保守的三态

服务对每条链接只回三种状态:

```jsonc
{
  "provider": "quark",            // 识别出的网盘;不支持则 "unknown"
  "status": "alive|dead|unknown", // 见下
  "reason": "share_ok",           // 机器可读的原因枚举(见 §3.2)
  "providerCode": "0",            // 上游原始码/标志,用于审计与排查
  "checkedAt": "2026-06-13T08:00:00Z",
  "cached": false
}
```

### 3.1 三态语义

| status | 何时返回 | 下游应如何用 |
|---|---|---|
| `alive` | 网盘 API **明确**表示分享存在且可访问(必要时已用提取码验证通过) | 可据此**驳回**用户的失效报告 |
| `dead` | 网盘 API **明确**表示:分享不存在 / 已删除 / 已取消 / 已过期 / 已被屏蔽 | **唯一**可据此自动标记资源失效的状态 |
| `unknown` | **其余一切** | **绝不**自动判失效,回退人工机制 |

### 3.2 reason 枚举(初版,可扩展)

- `alive`:`share_ok`(存在可访问)
- `dead`:`share_not_found`(不存在/已删除/已取消)、`share_expired`、`share_blocked`(违规屏蔽)
- `unknown`:`passcode_required`(需提取码但未提供/不正确)、`rate_limited`、`captcha_required`、`login_required`、`timeout`、`network_error`、`unsupported_provider`、`unparseable_response`(响应看不懂——API 可能变了)

### 3.3 保守策略(不确定就是 `unknown`)

**铁律**:只要不是"网盘明确说没了",一律 `unknown`,永不 `dead`。

- 缺提取码 / 提取码错 → `unknown`(不是 `dead`)。调用方应尽量带上库里存的提取码。
- 限流、验证码、超时、网络错误、5xx → `unknown`。
- **响应结构看不懂(上游改了 API)→ `unknown`**,并打日志/告警。
- 不支持的网盘(磁链/友站/图床/onedrive/mega…)→ `unknown` + `unsupported_provider`。

为什么这条最重要:这些网盘的 API **一定会变**。把"看不懂"当 `unknown` 而非 `dead`,意味着 API 漂移最坏只是**退化成人工兜底**,绝不会把一条好链接误杀。这是用"宁可漏报、绝不误报"换取下游可以**无条件信任** `dead`。

> 边界 case —— `share_blocked`(分享者被封/链接受限,如夸克 `41031`):用户视角确实打不开。**默认**归 `dead`(`share_blocked`);若要更保守,可配置成只认"分享不存在"类码、把 `blocked` 降级为 `unknown`。该开关写进配置。

---

## 4. 对外 API

### `POST /v1/check`
请求:
```json
{ "url": "https://pan.quark.cn/s/8476120a2a67", "passcode": "wdwV" }
```
`passcode` 可选(URL 里若自带 `?pwd=` 也会解析)。响应见 §3。

### `POST /v1/check/batch`
```json
{ "items": [ { "url": "...", "passcode": "..." }, ... ] }   // 上限 N 条
```
返回与输入等长的结果数组。批量内部仍受 per-provider 限流约束(可能比单条慢)。

### 鉴权
s2s,API Key(`Authorization: Bearer <key>` 或 Client Basic),每个消费方一把 key。**不对公网匿名开放**(否则成了帮人白嫖网盘探测的公共代理 + 招致封禁)。

### 同步 vs 异步
- 默认**同步**,带紧超时(如 8s);超时返回 `unknown/timeout`,绝不挂住调用方。
- 可选**异步**:`POST /v1/check/async` 入队 + 回调/轮询。用于批量复检等慢场景。Phase 1 可只做同步。

---

## 5. 架构

```
            ┌──────────────────────────────────────────┐
 consumer → │  HTTP API (/v1/check)  ── s2s auth        │
 (forum,    │        │                                  │
  patch)    │        ▼                                  │
            │   ┌─────────┐   命中? ┌──────────┐        │
            │   │  cache  │◀────────│  result  │        │
            │   └─────────┘         └──────────┘        │
            │        │ miss                              │
            │        ▼                                   │
            │   ┌─────────────┐  按 provider 限流/退避    │
            │   │  dispatcher │─────────────┐            │
            │   └─────────────┘             ▼            │
            │     provider registry   ┌───────────────┐ │
            │     quark / uc / baidu / │ ProviderChecker│ → 第三方网盘 API
            │     caiyun / xunlei /…   └───────────────┘ │
            └──────────────────────────────────────────┘
```

- **ProviderChecker 接口**(核心,语言无关地表达):
  ```go
  type Verdict struct {
      Status       Status // alive | dead | unknown
      Reason       string
      ProviderCode string
  }
  type Checker interface {
      // Matches 用 url 判断该 checker 是否负责此链接(host 模式)
      Matches(u *url.URL) bool
      Name() string
      Check(ctx context.Context, u *url.URL, passcode string) Verdict
  }
  ```
- **provider 注册表**:URL → 命中第一个 `Matches` 的 checker;都不命中 → `unknown/unsupported_provider`。
- **缓存**:键 = 规范化 URL(去无关 query)+ passcode 是否提供。TTL:`dead` 长(如 7d)、`alive` 中(如 12h)、`unknown` 短(分钟级)或不缓存。后端可先内存、后 Redis(多实例共享)。
- **限流**:每 provider 一个令牌桶 + 失败退避;命中网盘风控(验证码/限流码)时**主动降速**。
- **可观测**:每次校验记录 `provider/status/reason/providerCode/耗时`,便于发现 API 漂移(`unparseable_response` 飙升 = 某家改 API 了)。

---

## 6. 与下游的集成(报告闸门)

下游不直接信任用户点击,而是:

1. 用户报告资源失效 → 下游取该资源的链接 + 提取码 → 调 `POST /v1/check`。
2. 按返回:
   - `dead` → 立即标记资源失效(机器证实,单次报告即可),通知发布者附上具体原因。
   - `alive` → **驳回**本次报告:"经核验该链接仍可访问,未标记失效"。
   - `unknown` → 回退下游既有人工机制(法定人数 / 通知发布者 / 软状态)。
3. 多链接资源:可"全部 `dead` 才算失效"或"任一存活即不失效",由下游策略决定。

> 本服务**只回判定**,不碰下游的失效计数、通知、阈值、删除等——职责边界清晰。

---

## 7. 反爬 / 运维 / 合规

- **按需为主**:只校验被报告的链接,天然低频。**不做全量爬取**。
- **限流 + 缓存 + 退避**:控制对各网盘的请求速率;命中风控就降速;结果缓存避免重复打。
- **出口 IP 隔离**:跑抓取的 IP **必须**与 OAuth/身份服务分开。网盘封 IP 不得波及登录。(这正是它独立成服务、不进 infra 的根因。)
- **代理/IP 池**:后续可加,用于百度这类强反爬;初期单 IP + 低频 + 缓存先跑。
- **地域**:部分网盘 API 对非中国大陆 IP 行为不同。实测夸克 API 从生产服务器可直连成功(见 PROVIDERS.md)。部署位置需保证能正常到达目标网盘。
- **合规/ToS**:访问第三方网盘的非公开 API,属灰区。低频、按需、不破解凭证可降低风险;不对外开放匿名接口。仓库所有者据此决策。

---

## 8. 资源链接分布(实测)

来源:kun-galgame-forum 生产库 `galgame_resource_link`,共 **23,865** 条,2026-06-13 按 URL 域名归类。

| # | 网盘 / 类型 | 条数 | 占比 | 可客观校验 |
|---|---|---|---|---|
| 1 | 百度网盘 `pan.baidu.com` | 8492 | 35.6% | ✅(反爬最凶) |
| 2 | 夸克网盘 `pan.quark.cn` | 6882 | 28.8% | ✅(已实测) |
| 3 | UC 网盘 `drive.uc.cn` | 1661 | 7.0% | ✅(夸克同源) |
| 4 | 和彩云 `caiyun.139.com` | 1584 | 6.6% | ✅ |
| 5 | 迅雷网盘 `pan.xunlei.com` | 912 | 3.8% | ✅ |
| 6 | OneDrive / SharePoint | ~600 | ~2.5% | ⚠️ 部分 |
| 7 | 123 盘 | 344 | 1.4% | ✅ |
| 8 | 磁力链 `magnet:` | 337 | 1.4% | ❌ 无 HTTP |
| 9 | PikPak | 203 | 0.85% | ⚠️ |
| 10 | mega / 蓝奏 / 天翼 / 阿里 … | 少量 | <1% | 部分 |
| — | 友站/图床/聚合分享页(inari.site、nekogal、loli520、lycorisgal、mikugame、zi6/zi0、touchgal …) | ~3.5k | ~15% | ❌ 各异 |

**前 5 家网盘合计 ≈ 81.8%;加 123 盘 ≈ 83.2%。** 先吃这部分,尾巴回退人工。
> 注:`p.inari.site`(813 条)是 `.png` 结尾,疑似被误当资源链接的图片;下游可顺手清理,与本服务无关。

完整分类 SQL 与各 host 明细见 [PROVIDERS.md](PROVIDERS.md)。

---

## 9. 路线图

| 阶段 | 内容 | 备注 |
|---|---|---|
| Phase 0 | 骨架:接口、HTTP API、s2s、缓存、限流、配置、可观测 | provider 无关的地基 |
| Phase 1 | **夸克 + UC** checker（~36%，API 已验证）+ 接成论坛报告闸门 | 端到端最小闭环 |
| Phase 2 | **百度**（35.6%，最难） | 框架成熟后 |
| Phase 3 | 和彩云 / 迅雷 / 123 盘 | |
| 后续 | 后台低频复检、代理/IP 池、PikPak/天翼/蓝奏、尾巴策略 | 按收益排期 |

---

## 10. 待决策(开放问题)

1. `share_blocked`(受限/封禁)默认归 `dead` 还是 `unknown`?(本文默认 `dead`,可配置)
2. 是否需要异步端点 / 后台低频复检(初期建议先不做)。
3. 缓存后端:先内存,还是直接上 Redis(多实例共享)?
4. 部署形态与出口 IP 方案(单 IP + 低频 vs 代理池)。
5. ~~License 选型~~ → **AGPL-3.0**(已定,见 LICENSE)。
