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

状态码映射(初版,随实测补全):

| code | 含义 | 判定 |
|---|---|---|
| `0` | ok,拿到 `stoken` | `alive` / `share_ok` |
| `41008` | 需要提取码 | `unknown` / `passcode_required`（带上 pwd 重试） |
| `41031` | 分享者被封 / 链接受限 | `dead` / `share_blocked`（可配置降级） |
| `41006` / `41027` / 其它"分享不存在/已取消/已失效"类 | 分享已删除 / 失效 | **`dead` / `share_not_found`** ⚠️ **待用已知失效分享确认确切 code** |
| 其它未知 code | —— | **`unknown` / `unparseable_response`** |

> Phase 1 必做:找一条**已知已删除**的夸克分享,确认"不存在/已删除"的确切 code,补进上表。在确认前,只有 `41031` 是已验证的 `dead`-类;其余一律 `unknown`。

带提取码的正确姿势:`passcode` 用资源里存的提取码(或 URL 的 `?pwd=`)。先不带 → 若 `41008` 则带上重试 → 仍非 `0` 再按码判定。

---

## UC 网盘 uc(7.0%)✅ 夸克同源,待对参数

- 分享 URL:`https://drive.uc.cn/s/<pwd_id>`
- UC 与夸克同属阿里/UC 系,后端高度同构,**预计同一套 `clouddrive/share/sharepage/token` API**,只是 host / `pr` / `fr` 参数不同(UC 端常见 `pr=UCBrowser`)。
- 实测:用夸克的参数直接打 `drive.uc.cn/1/clouddrive/share/sharepage/token` 返回 `403 Forbidden`(HTML)——**参数没对**,不是失效信号。Phase 1 需找到 UC 正确的 API host/参数(可能是 `pc-api.uc.cn` 之类),拿到与夸克同构的 code 后复用映射。

---

## 百度网盘 baidu(35.6%)✅ 可行但最难

- 分享 URL:`https://pan.baidu.com/s/<surl>`(常带 `?pwd=<提取码>`)
- **反爬最凶**:裸 `curl` 实测直接 `302`(body 仅 135 字节),重定向到验证/登录。需要正确的 API + headers(UA、Referer、可能要 `BAIDUID` 之类 cookie)。
- 思路(待 Phase 2 落地与验证):
  - 短链信息 / 校验接口(如 `https://pan.baidu.com/share/verify`、`/api/shorturlinfo`、`/share/init`)对**失效/取消**的分享会返回确定性 `errno`(社区常见:`-1` 无效、`-9` 提取码错、`116`/`105` 等链接不存在/取消)。
  - 页面层面也有确定文案:`分享的文件已经被取消`、`你访问的页面不存在`、`该链接分享内容可能因为涉及……无法访问`。
- ⚠️ 上述 `errno` 含义需**实测确认**后再写死映射;确认前一律 `unknown`。百度建议放到框架成熟、且准备好 headers/cookie/退避之后再做。

---

## 和彩云 caiyun(6.6%)· 迅雷 xunlei(3.8%)· 123 盘(1.4%)—— Phase 3

各自有分享详情/校验 API,返回确定性的"分享存在 / 不存在 / 需提取码"状态。逐家实测后补本档:
- 和彩云 `caiyun.139.com`(中国移动)
- 迅雷 `pan.xunlei.com`
- 123 盘 `123pan` 系(多个马甲域名 `123684/123865/123912.com` 等)

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
