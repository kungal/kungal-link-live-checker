# 各网盘逆向参考 / 状态码映射

> 这是一份**活文档**:每家网盘的分享状态 API、状态码 → 判定 的映射、以及反爬/坑点。网盘 API 会变,这里记录"截至某日实测"的事实。**实现里任何"看不懂的响应"都必须归 `unknown`,绝不归 `dead`。**

实测环境:从 kun-galgame-forum 生产服务器(可直达国内网盘)用 `curl` 探测,2026-06-13。

图例:`alive` = 确定有效 · `dead` = 确定失效 · `unknown` = 无法判定(回退人工)。

---

## 夸克网盘 quark(28.8%)✅ 已验证

- 分享 URL:`https://pan.quark.cn/s/<pwd_id>`(可带 `?pwd=<passcode>`)
- **不要抓 HTML**:HTML 是 SPA 空壳(实测三条不同分享的 HTML 长度都是 9339,真实状态在 JS 调的 JSON API)。
- 状态 API(实测可用):
  ```
  POST https://drive-pc.quark.cn/1/clouddrive/share/sharepage/token?pr=ucpro&fr=pc
  Content-Type: application/json
  Referer: https://pan.quark.cn/
  Body: {"pwd_id":"<pwd_id>","passcode":"<提取码或空串>"}
  ```

实测返回(2026-06-13,prod 直连):

| pwd_id | 响应(截断) | 判定 |
|---|---|---|
| `eb34b875e97f` | `{"status":200,"code":0,"message":"ok",...,"stoken":"...","expired_at":4102416000000}` | **alive** |
| `8476120a2a67` | `{"status":404,"code":41008,"message":"需要提取码",...}` | **unknown**(`passcode_required` —— 我没带提取码;带上正确 pwd 应得 `code:0`) |
| `a860b9c42557` | `{"status":403,"code":41031,"message":"分享者用户封禁链接查看受限",...}` | **dead**(`share_blocked`,默认;保守模式可降级 `unknown`) |

状态码映射(**已实测确认**,2026-06-13):

| code | message | 判定 | reason |
|---|---|---|---|
| `0` | `ok`(且 `data.stoken` 非空) | `alive` | `share_ok` |
| `41008` | 需要提取码 | `unknown` | `passcode_required`(带上 pwd 重试) |
| `41004` | 文件不存在 | **`dead`** | `share_not_found` |
| `41006` | 分享不存在 | **`dead`** | `share_not_found` |
| `41011` | 分享地址已失效 | **`dead`** | `share_expired` |
| `41012` | 好友已取消了分享 | **`dead`** | `share_not_found` |
| `41010` | 文件涉及违规内容 | **`dead`**(可配置降级) | `share_blocked` |
| `41031` | 分享者用户封禁链接查看受限 | **`dead`**(可配置降级) | `share_blocked` |
| 其它未知 code | —— | **`unknown`** | `unparseable_response`(并打 WARN 日志) |

**实测来源**:对论坛库 `galgame_resource_link` 里最早一批 57 条夸克分享逐条打 token API(2026-06-13,prod 直连),得到上述分布——`0`×27、`41008`×17、`41004`×10、`41011`×2、`41012`×1;`41006` 用不存在的 pwd_id 复现。这些"失效类"码的文案都明确无歧义("文件不存在/分享不存在/已失效/已取消"),且与 `41008`(缺/错提取码)**完全不重叠**,故映射成 `dead` 无误杀风险。

> 说明:`41010`/`41031` 属"违规/封禁下架"(用户视角同样打不开),默认 `dead`,可经 `LLC_QUARK_BLOCKED_AS_DEAD=false` 降级为 `unknown`。`41027` 文档曾猜测是失效类,但**实测样本里从未出现**,按铁律保持 `unknown`,直到亲眼实测再升级。

带提取码的正确姿势:`passcode` 用资源里存的提取码(或 URL 的 `?pwd=`)。先不带 → 若 `41008` 则带上重试 → 仍非 `0` 再按码判定。

### ⚠️ `code:0` 不等于有效:必须再打 detail(2026-08-11 实测补充)

**token 的 `code:0` 只证明提取码被接受,不证明还有东西可下。** 违规下架的分享照样返回 `code:0` + 合法 `stoken`。

事故:`pan.quark.cn/s/2fce2f1d6d91`(提取码 `Tyi1`)被本服务判 `alive`,而浏览器输入提取码后显示"该分享已失效,不可访问"。

第二步(取到 `stoken` 后):

```
GET https://drive-pc.quark.cn/1/clouddrive/share/sharepage/detail?pr=ucpro&fr=pc
    &pwd_id=<pwd_id>&stoken=<stoken>&pdir_fid=0&_page=1&_size=1&_fetch_share=1
Referer: https://pan.quark.cn/
```

`_size=1` 足够拿到 `data.share`(响应 ~3KB),不必拉整个文件列表。

实测对照(4 条违规 + 8 条健康,论坛库真实链接):

| pwd_id | `share.status` | `partial_violation` | `violation_cnt` | 根目录条目 | 下钻一层 | 真实状态 |
|---|---|---|---|---|---|---|
| `2fce2f1d6d91` | 1 | **true** | 1 | 1 | **0** | 已失效 |
| `c9840931569a` | 1 | **true** | 3 | 1 | **0** | 已失效 |
| `8fc80ea2bf28` | 1 | **true** | 2 | 1 | 1 | 部分违规 |
| `a0a13a16fcf6` | 1 | **true** | 1 | 1 | 5 | 部分违规,仍列得出文件 |
| 8 条健康链接 | 1 | false | 字段缺失 | 1 | 3–6 | 有效 |

被排除的字段,别再走弯路:

- **`audit_status` 不是信号**:健康链接也是 `4`,违规链接出现过 `1`。四个社区实现全都不用它。
- **`share.status` 在本样本里恒为 `1`**,不区分。社区实现依赖的 `status>1` 分支在这里根本不触发。
- **`partial_violation` 不可判 `dead`**:它同时覆盖"全空"和"删了几个、其余照常下载"两种情况,本层响应无法区分。

映射(`LLC_QUARK_VERIFY_DETAIL=true`,默认开):

| detail 响应 | 判定 | reason |
|---|---|---|
| `code:0` 且 `partial_violation` 缺失/为 false | `alive` | `share_ok` |
| `code:0` 且 `partial_violation == true` | **`unknown`** | `share_blocked`(码 `0/partial_violation`) |
| `code != 0` / 非 JSON / 网络错误 / 超时 | `unknown` | 对应 reason |

**为什么是 `unknown` 而不是 `dead`**:`a0a13a16fcf6` 违规标记为真却仍列得出 5 个文件,判 `dead` 就是误杀。按铁律,分不清就是 `unknown`。代价是这部分链接失去"当场驳回误报"的能力;收益是随机 27 条抽样里 3 条(token-alive 的 12.5%)假 `alive` 被消除。关掉开关即回退到只看 token 的旧行为。

**待定(§10 类,需仓库主人拍板)**:根目录只有单个文件夹时下钻一层,若为空且 `partial_violation` 则判 `dead`。能捞回上表前两条,但依据只有 n=2,且空文件夹分享是合法存在的,暂不实现。

社区参考实现(判定逻辑收敛,但**都只查根目录**,对"单顶层文件夹"型分享会和我们最初一样误判 alive):[Lampon/PanCheck](https://github.com/Lampon/PanCheck)(Go)、[fish2018/pansou](https://github.com/fish2018/pansou)(Go)、[iflymeto/yuexin-search](https://github.com/iflymeto/yuexin-search)(PHP)、[WhiteSevs/TamperMonkeyScript](https://github.com/WhiteSevs/TamperMonkeyScript)(TS)。注意 pansou 会对 `message` 做关键词匹配("不存在/失效/违规")——**本项目不采纳**,文案随时会改,看不懂就得是 `unknown`。

---

## UC 网盘 uc(7.0%)✅ 已验证,夸克同源

- 分享 URL:`https://drive.uc.cn/s/<pwd_id>`(常带 `?public=1` 等无关 query)
- 状态 API(**实测可用**,与夸克同构,仅 host/参数不同):
  ```
  POST https://pc-api.uc.cn/1/clouddrive/share/sharepage/token?pr=UCBrowser&fr=pc
  Content-Type: application/json
  Origin: https://drive.uc.cn          ← UC 必须带 Origin(夸克不需要)
  Referer: https://drive.uc.cn/
  Body: {"pwd_id":"<pwd_id>","passcode":"<提取码或空串>"}
  ```
- 返回 envelope 与夸克**完全一致**(`{"status","code","message","data":{"stoken"}}`),**复用夸克那张状态码映射表**。无需登录 cookie 即可探测。
- 实测来源:对论坛库 25 条最早 UC 分享逐条打上述端点(2026-06-13):`0`×17(alive)、`41008`×5(需提取码)、`41010`×2(文件涉及违规内容 → blocked/dead)、`41012`×1(好友已取消 → dead)。与夸克码族同源,确认无误。
- 早先"用夸克参数直接打 `drive.uc.cn` 得 403 HTML"是**参数/host 没对**,不是失效信号;换到 `pc-api.uc.cn?pr=UCBrowser` + `Origin` 后即正常。
- **detail 二次校验同样适用**(2026-08-11 实测):`GET https://pc-api.uc.cn/1/clouddrive/share/sharepage/detail?pr=UCBrowser&fr=pc`,参数与夸克一致,返回同一 schema(含 `partial_violation`)。6 条真实 UC 分享全部 `code:0` / `partial_violation:false`,与夸克共用上面那张 detail 映射表。端点可经 `LLC_UC_DETAIL_URL` 覆盖。

---

## 百度网盘 baidu(35.6%)✅ 已验证(走 shorturlinfo)

- 分享 URL:`https://pan.baidu.com/s/1<surl>`(常带 `?pwd=<提取码>`)
- **反爬**:裸 `curl` 无 cookie 直接 `302` 到反爬页。但**只要先拿一个 `BAIDUID` cookie**(GET 一次 `https://pan.baidu.com/` 即由服务端 `Set-Cookie`),后续 `shorturlinfo` **无需登录**即可读 JSON。这是本服务采用的方案。
- 状态 API(**实测可用**,2026-06-13):
  ```
  GET https://pan.baidu.com/api/shorturlinfo?app_id=250528&web=1&channel=chunlei&clienttype=0&shorturl=1<surl>
  Cookie: BAIDUID=...        ← 先 GET 首页拿到
  User-Agent: <浏览器 UA>
  Referer: https://pan.baidu.com/s/1<surl>
  ```
- **`errno` 映射**(本服务用的就是这张表):

  | `errno` | 含义(实测) | 判定 |
  |---|---|---|
  | `0` | 公开分享存在可访问(且响应**带 errno 字段**) | `alive` / `share_ok` |
  | `-9` | 带提取码分享(**本层分不出死活**) | → **取分享页判定**(见下) |
  | `-21` | 公开分享不存在(标题"链接不存在") | **`dead` / `share_not_found`** |
  | `140` | 链接格式错误("啊哦,链接出错了",`shareid:0`) | `unknown`(可能是畸形输入) |
  | 缺 `errno` 字段 / 其它 | —— | `unknown` / `unparseable_response`(打 WARN) |

- **`-9`(带密分享)靠分享页判定**(2026-06-26 定):`shorturlinfo` 的 `-9` 对**失效的带密分享**和**有效的带密分享**返回同一个码(甚至同一条死链会在 `-9`/`-21` 间抖动),这一层判不了。于是 `-9` 时**再 GET 一次 `https://pan.baidu.com/s/1<surl>?pwd=<pwd>`**(跟随跳转),看**服务端渲染**的哨兵(实测互斥、稳定):

  | 分享页含 | 判定 |
  |---|---|
  | `链接不存在` | **`dead` / `share_not_found`** |
  | `请输入提取码`(即跳到提取码页,说明分享存在·被锁) | `alive` / `share_ok`(存在即非 dead) |
  | 都没有 | `unknown` / `passcode_required`(保守) |

  实测 5 条(2 死 3 活)哨兵 100% 互斥且服务端直出(非 SPA 空壳),故**既不误判 dead 也不误判 alive**。这是对"不抓 HTML"原则的**有据例外**:仅用于 `-9` 这一歧义分支、且两向都靠**正向哨兵**(缺则 `unknown`)。
- ⚠️ 陷阱:`/share/verify` **不能判死活**——对已删分享只要提取码对照样回 `errno:0`+`randsk`(它只验提取码对不对);`shareid`/`uk` 从 shorturl 解码,死链照样有;响应**缺 `errno`** 也绝不 alive(防空壳当 `errno:0`)。
- **实测来源**:论坛库最早 48 条(2026-06-13)+ 两条用户报告的失效带密分享(2026-06-26)。

<details><summary>备用:带提取码校验 / 转存用的 <code>/share/verify</code>(Phase 1 闸门暂不需要,留作后续)</summary>

`POST https://pan.baidu.com/share/verify?surl=<surl>&bdstoken=<t>&channel=chunlei&web=1&clienttype=0`,body `pwd=<提取码>`;成功回 `randsk`(写 `BDCLND` cookie)。此端点 `errno`:`-9`=提取码错误(**unknown,非 dead**)、`-12/-62`=错误次数过多/验证码(unknown)、`-7`=已删除/取消、`-8`=已过期。需 `bdstoken`(从 `/api/gettemplatevariable` 取)。来源:`void285` gist、`PeterDing/iScript` wiki、`hxz393/BaiduPanFilesTransfers`。**这些码未在本服务实测,用前需复验。**

</details>

---

## 和彩云 caiyun(6.6%)✅ 已验证

- 分享 URL:`https://caiyun.139.com/m/i?<linkID>`(也有 `front/#/detail?linkID=<id>`,linkID 在 fragment)
- 状态 API(**实测可用,明文、无需登录**——与社区传"必须 AES + Basic 鉴权"不同,读外链信息走明文即可):
  ```
  POST https://share-kd-njs.yun.139.com/yun-share/richlifeApp/devapp/IOutLink/getOutLinkInfoV6
  Content-Type: application/json
  Referer: https://yun.139.com/
  Body: {"getOutLinkInfoReq":{"linkID":"<linkID>","pCaID":"root"}}
  ```
- 返回 `{"code":"<字符串码>","desc":"...","success":bool,"data":...}`。**`code` 映射**(实测):

  | `code` | desc | 判定 |
  |---|---|---|
  | `0` | (有 data) | `alive` / `share_ok` |
  | `9188` | 提取码非法 | `alive`(分享存在·被提取码锁;存在即非 dead——见 §3.1 决策) |
  | `200000727` | 外链不存在/外链被分享者取消 | **`dead` / `share_not_found`** |
  | 其它 | —— | `unknown` |

- 实测来源:论坛库 29 条最早和彩云分享(2026-06-13):`9188`×20、`0`×7、`200000727`×2。

## 123 盘 123pan(1.4%)✅ 已验证

- 分享 URL:`https://www.123pan.com/s/<key>`,马甲域名 `123912/123684/123865.com`、`123pan.cn`(**每个马甲是独立部署,API 必须打同一个 host**);也有个人子域 `<num>.share.123pan.cn/123pan/<key>`。
- 状态 API(**实测可用,无需登录/无需签名**):`GET https://<同链接 host>/b/api/share/get?shareKey=<key>&SharePwd=<提取码>&...`,头带 `platform: web`、`app-version: 3`。
- 返回 `{"code":<int>,"message":"...","data":...}`。**⚠️ `code 5103` 一码两义,必须看 `message` 区分**:

  | `code` + `message` | 判定 |
  |---|---|
  | `0` | `alive` / `share_ok` |
  | `5103` + `此分享不存在` | **`dead` / `share_not_found`** |
  | `5103` + `提取码错误` | `alive`(分享存在·被锁;先用提取码 `SharePwd` 试,仍错则存在即非 dead) |
  | `5103` + 其它 message | `unknown` |
  | 其它 code | `unknown` |

- 实测来源:论坛库多个马甲的 `/s/` 分享(2026-06-13)同时见到 `5103 此分享不存在`(dead)与 `5103 提取码错误`(alive)——**社区传的"5103=分享不存在"是半对:不看 message 直接判 dead 会误杀所有带密分享**。源(端点形态):`AlistGo/alist drivers/123_share`。

## 迅雷 xunlei(3.8%)⏸ 暂缓(需设备签名+验证码+登录)

- 分享 URL:`https://pan.xunlei.com/s/<share_id>`(常带 `?pwd=`)。
- `GET https://x-api-pan.xunlei.com/drive/v1/share?share_id=<id>` 实测直接回 `{"error":"invalid_argument","error_code":3,"...":"device_id is empty"}`——**需要 `x-device-id` + `x-captcha-token`(captcha init 流程)+ 登录态**,无法匿名探测。错误 envelope `{"error","error_code","error_description"}`;`captcha_invalid` 属 `unknown` 非 dead。源:`power721/alist drivers/thunder_share`、`AlistGo/alist drivers/thunder_browser`。
- 现状:**未实现**,迅雷链接走 `unknown / unsupported_provider`。投入产出比低(3.8% 且需逆向设备签名),留待后续;实现前必须像其它家一样先拿"已知失效分享"实测确认 dead 码。

---

## 不支持(尾巴 ~16%)→ 一律 `unknown / unsupported_provider`

下游对这些回退人工机制:

- **磁力链** `magnet:` —— 无 HTTP 端点,客观上无法探测。
- **OneDrive / SharePoint**(`*-my.sharepoint.com`)、**mega.nz**、**Google Drive** —— 海外盘,可探但优先级低、收益小。
- **PikPak**(`mypikpak.com`)、**蓝奏云**、**天翼云**、**阿里云盘**、**微云**(`share.weiyun.com`)、**Telegram**(`t.me`)—— 少量,按收益再说。
- **友站 / 聚合分享页 / 图床**:`p.inari.site`、`share.nekogal.top`、`gal.loli520.cc`、`www.lycorisgal.com`、`mikugame.*`、`zi6.cc`/`zi0.cc`、`drives.ykkit.cn`、`touchgal.*`、`shinnku.com` 等 —— 各站结构各异,无统一 API;一般不值得逐站做。

---

## 附:分类用 SQL(论坛库 `galgame_resource_link`)

```sql
-- 按域名归类统计占比
with c as (
  select case
    when url ~* '(pan\.baidu\.com|baidu\.com)' then '百度'
    when url ~* 'pan\.quark\.cn|quark\.cn'      then '夸克'
    when url ~* 'drive\.uc\.cn'                 then 'UC'
    when url ~* '123pan|123\d{3}\.com'          then '123盘'
    when url ~* 'pan\.xunlei|xunlei\.com'       then '迅雷'
    when url ~* 'caiyun\.139|yun\.139|139\.com' then '和彩云'
    when url ~* 'lanzou'                        then '蓝奏'
    when url ~* 'alipan|aliyun'                 then '阿里'
    when url ~* 'cloud\.189|189\.cn'            then '天翼'
    when url ~* '^\s*magnet:'                   then '磁链'
    when url ~* 'mega\.nz'                      then 'mega'
    when url ~* 'onedrive|1drv|sharepoint'      then 'onedrive'
    else '其他/未识别'
  end as provider, count(*) n
  from galgame_resource_link group by 1)
select provider, n, round(100.0*n/sum(n) over (),1)||'%' pct from c order by n desc;

-- 看"未识别"桶里都是哪些 host(决定要不要补 provider)
with u as (
  select lower(substring(url from '^[a-z]+://([^/]+)')) host
  from galgame_resource_link
  where url !~* 'baidu|quark|drive\.uc|123pan|123\d{3}|xunlei|caiyun|139\.com|lanzou|aliyun|189\.cn|^\s*magnet:|mega\.nz|onedrive|1drv|sharepoint')
select host, count(*) n from u group by 1 order by 2 desc limit 30;
```
