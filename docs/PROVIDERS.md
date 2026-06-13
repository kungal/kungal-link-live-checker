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

---

## 百度网盘 baidu(35.6%)✅ 可行但最难

- 分享 URL:`https://pan.baidu.com/s/<surl>`(常带 `?pwd=<提取码>`)
- **反爬最凶**:裸 `curl` 实测直接 `302`(body 仅 135 字节),重定向到验证/登录。需要正确的 API + headers(UA、Referer、可能要 `BAIDUID` 之类 cookie)。
- 接口与字段(社区文档一致度高,字段名 `errno`;**仍需带 cookie 实测复验**后再写死):
  - 提取码校验:`POST https://pan.baidu.com/share/verify?surl=<surl>&bdstoken=<t>&t=<ms>&channel=chunlei&web=1&clienttype=0`,body `pwd=<提取码>`;成功回 `randsk`(写入 `BDCLND` cookie)。
  - 分享页/有效性:`GET https://pan.baidu.com/s/<surl>`,从返回里抓 `shareid`/`share_uk`/`fs_id`;抓不到即失效。`<title>` 也带状态:**`百度网盘-链接不存在`**。
  - **需要登录态**:`Cookie` 至少含 `BAIDUID`(转存还需 `BDUSS`)、`bdstoken`(从 `/api/gettemplatevariable` 取)、真实浏览器 UA、`Referer: https://pan.baidu.com`。裸 curl 会被 302 到登录页。

  | `errno` | 含义 | 判定 |
  |---|---|---|
  | `0` | 成功 / 提取码正确 | `alive`(配合抓到 shareid) |
  | `-7` | 分享已删除或已取消 | **`dead` / `share_not_found`** |
  | `-8` | 分享已过期 | **`dead` / `share_expired`** |
  | `-9` | **提取码错误** | `unknown` / `passcode_required`(**绝非 dead**,经典误杀点) |
  | `-12` / `-62` | 提取码错误次数过多 / 需验证码 | `unknown` |
  | `-19` / `-20` | 验证码 | `unknown` / `captcha_required` |
  | `-16` | 文件已被限制分享 | `dead`(倾向,待确认不是临时态) |
  | 其它 | —— | `unknown` |

- 来源:`void285` errno gist、`PeterDing/iScript` wiki、`hxz393/BaiduPanFilesTransfers`(`verify_links`/`verify_pass_code`,直接读到上述流程与 title 哨兵)。⚠️ 仍按铁律:Phase 2 带 cookie 实测复验 `-7/-8/-16` 后再开 `dead`,确认前一律 `unknown`。`-9` 必须是 `unknown`。

---

## 和彩云 caiyun(6.6%)· 迅雷 xunlei(3.8%)· 123 盘(1.4%)—— Phase 3

各自有分享详情/校验 API,返回确定性的"分享存在 / 不存在 / 需提取码"状态。逐家实测后补本档。已检索到端点(失效码均**待实测**,开源工具普遍只做 `code!=0 即失败`,不区分"已删除 vs 限流",这正是本服务要补的):

- **和彩云 `caiyun.139.com`**(中国移动):`POST https://share-kd-njs.yun.139.com/yun-share/richlifeApp/devapp/IOutLink/getOutLinkInfoV6`,body 含 `linkID`;**请求/响应是 AES-128-CBC**(硬编码 key `PVGDwmcvfs1uV3d1`)、需 `Authorization: Basic` 登录态。envelope `{"success":bool,...}`,失效表现为 `success:false` + message(确切串待抓)。源:`AlistGo/alist drivers/139`。
- **迅雷 `pan.xunlei.com`**:`GET https://x-api-pan.xunlei.com/drive/v1/share?share_id=<id>&pass_code=<code>`;需 `x-captcha-token`(captcha init 流程)+ 设备签名 + 登录态。错误 envelope `{"error","error_code","error_description"}`;注意 `captcha_invalid` 是 `unknown` 不是 dead。源:`power721/alist drivers/thunder_share`。
- **123 盘 `123pan` 系**(马甲域名 `123684/123865/123912.com` 等):`GET https://www.123pan.com/b/api/share/get?shareKey=<key>&SharePwd=<code>`;envelope `{"code","message","data"}`,`code:0` = alive,需签名 query + `platform:web` 头。失效码待实测(社区传 `5103` **未证实,勿用**)。源:`AlistGo/alist drivers/123_share`、`Y-ASLant/123pan`。

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
