# CLAUDE.md — 给接手本项目的 AI 代理

> 这是一个 **Go 微服务**:给定一个网盘分享链接(可带提取码),用各网盘**自家的分享状态 JSON API** 客观判断它 **有效 / 已失效 / 无法判定**。目的是替代下游"用户主观报告即失效"的误报机制。

## 先读这三份(顺序别错)
1. **`README.md`** —— 项目门面:为什么做、80/20 数据、三态契约、报告闸门、路线图。
2. **`docs/REQUIREMENTS.md`** —— 权威需求与设计。**实现以它为准。** 含 API、架构、集成、反爬运维、实测分布、开放问题。
3. **`docs/PROVIDERS.md`** —— 各网盘逆向参考 + 状态码映射(含实测证据:夸克 `code` 表、百度反爬、分类 SQL)。

读完这三份你就掌握全部背景。本文件只补充"作为代理你要特别记住的东西"。

## 当前状态
- **只有文档,没有代码。** 你接手时仓库是设计骨架(README + docs + go.mod + .gitignore)。
- 你的第一项工作通常是 **Phase 0(骨架)→ Phase 1(夸克 + UC)**,见 README/REQUIREMENTS 路线图。
- module path:`github.com/KunMoe/kungal-link-live-checker`(Go 1.23)。栈追求**最小依赖、单二进制**;HTTP 用 stdlib `net/http` 即可,别无脑引重框架。

## 不可违反的铁律(整个项目的命根子)

**保守三态:`alive` / `dead` / `unknown`。**

1. **只有**网盘 API **明确返回"分享不存在 / 已删除 / 已取消 / 已失效 / 已屏蔽"**时,才返回 `dead`。
2. **其余一切**——缺提取码、限流、验证码、超时、网络错误、不支持的网盘、**任何看不懂/没见过的响应**——**一律 `unknown`,永不 `dead`。**
3. 原因:下游会**仅凭 `dead` 就自动把资源标记失效**。网盘 API 一定会变;只要你坚持"看不懂=unknown",API 漂移最坏只是退化成人工兜底,**绝不会误杀一条好链接**。这是用"宁可漏报、绝不误报"换下游对 `dead` 的无条件信任。
4. 写任何 provider checker 时,先问自己:"这个分支会不会在'我其实不确定'的时候吐 `dead`?" 如果会,改成 `unknown`。

违反这条 = 把项目的核心价值(消灭误报)亲手毁掉。

## 实现要点(别踩的坑)
- **不要抓 HTML 找关键词**。网盘是 SPA + 反爬:百度裸 curl 直接 `302`、夸克 HTML 是恒定长度的空壳。真实状态在它们的 **JSON API**里。逐家逆向那个 API。
- **带上提取码**:调用方会传 `passcode`(或 URL 的 `?pwd=`)。加密分享不带码会得到"需提取码",那是 `unknown` 不是 `dead`——务必先带码重试。
- **provider 插件化**:每家一个实现 `Checker` 接口(`Matches(url)` / `Name()` / `Check(ctx,url,passcode) → Verdict{Status,Reason,ProviderCode}`)。URL 不命中任何 provider → `unknown / unsupported_provider`。核心**不掺任何下游业务**(失效计数/通知/阈值都在下游)。
- **按需、低频、带缓存与退避**。**绝不做全量爬取**(全量必被网盘封 IP)。`dead` 长缓存、`alive` 中等、`unknown` 极短或不缓存。
- **s2s 鉴权**,每个消费方一把 key;**绝不**对公网匿名开放(否则成了公共网盘探测代理 + 招封)。
- **密钥绝不入库**:API key / 代理凭证走 env / 配置文件,已在 `.gitignore` 里挡掉 `.env*`、`config.local.*`。

## 如何验证 provider 行为(关键)
provider 的状态码是**实测得来**的,不是猜的。`docs/PROVIDERS.md` 记了截至 2026-06-13 的实测结果。你要**亲自对真实 API 复验**再写死映射。例如夸克(已验证可直连):

```bash
curl -s -X POST "https://drive-pc.quark.cn/1/clouddrive/share/sharepage/token?pr=ucpro&fr=pc" \
  -H "Content-Type: application/json" -H "Referer: https://pan.quark.cn/" \
  --data '{"pwd_id":"<分享id>","passcode":"<提取码或空>"}'
# code:0=有效  41008=需提取码(unknown)  41031=受限(默认dead)  分享不存在的确切 code 待你用"已知失效分享"确认
```

注意:① 部分网盘 API 对**非中国大陆 IP** 行为不同,本机复验若异常,先排除地域/网络因素;② **Phase 1 必做**:找一条**确定已删除**的分享,确认"不存在/已删除"的确切返回码,补进 PROVIDERS.md——在确认前,只有已验证的码才能映射成 `dead`,其余 `unknown`。

## 消费方背景(你不必动它,但要知道)
第一个消费方是 **kun-galgame-forum**(论坛)。它把本服务接成"用户报告失效"的**闸门**:`dead`→自动失效、`alive`→当场驳回误报、`unknown`→回退它自己的人工机制。也就是说本服务**只回判定,不碰下游业务**。集成细节见 REQUIREMENTS §6。未来 moyu(补丁站)也会复用。

## 范围纪律(别过度,也别跑偏)
- **先吃大头**:夸克/UC(~36%,API 已验证)→ 百度(35.6%,最难,框架成熟后再啃)→ 和彩云/迅雷/123。磁链/onedrive/mega/友站/图床这 ~16% 的尾巴**一律 `unknown`**,留给下游人工,**别**一开始就去逐站做。
- **复杂度要由收益偿付**:别提前上代理池/异步/Redis/后台复检——这些在 REQUIREMENTS §10 是"待决策",等真有需要再上。Phase 1 单 IP + 同步 + 内存缓存就够跑通。
- 有歧义的设计抉择(如 `41031 受限`算 `dead` 还是 `unknown`、缓存后端、异步)在 REQUIREMENTS §10 列着——**先问仓库所有者**,别擅自定。

## 一句话
你交付的核心价值是:**对支持的网盘,只在 100% 确定失效时说 `dead`;其余诚实地说 `unknown`。** 守住这条,其余都是工程细节。
