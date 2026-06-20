# CLAUDE.md — For the AI Agent Taking Over This Project

## 铁律 (Iron Rules — non-negotiable; these override every other guideline in this file)

1. **Commit, but do not push.** Commit changes whenever appropriate, but do not run `git push` on your own initiative — the user pushes. When a push is genuinely required, and especially when several repos must be pushed in a specific order, stop and tell the user the exact push order instead of pushing yourself.
2. **No background gradients in any UI, ever.** Never use gradient backgrounds in UI design (`bg-gradient-*`, `from-*/via-*/to-*`, `linear-gradient()`, `radial-gradient()`, `conic-gradient()`, etc.); use solid colors from the project's palette.


> This is a **Go microservice**: given a cloud-drive share link (optionally with a passcode), it uses each cloud drive's **own share-status JSON API** to objectively determine whether the link is **alive / dead / unknown**. The goal is to replace the downstream "a user's subjective report = dead" mechanism that produces false positives.

## Core Engineering Principles

> Shared baseline across all KUN Galgame repositories. Defaults, not dogma — apply judgment.

1. All commit messages must be written entirely in English.
2. All code comments must be written entirely in English.
3. Keep each source file under ~500 lines where practical; once a file grows past ~300 lines, consider splitting it (a guideline, not a hard rule).
4. Write every frontend function as an arrow function; compose/merge class names with `cn` wherever practical.
5. Deliberately balance elegant modularity against necessary duplication — choose per case instead of always favoring either.
6. Constantly verify that frontend and backend agree on the data: field shapes and response formats must match what each side expects.
7. After every change, watch for unintended side effects elsewhere.
8. If a change requires running a migration, tell the user explicitly at the end — which command, and against which database.
9. Always seek the most modern, elegant solution that fits the project's current state; consult the latest official docs and resources online when useful.
10. Never let the pursuit of elegance or modularity make the code complex or hard to follow, and don't write over-defensive code.

## Read These Three First (don't get the order wrong)
1. **`README.md`** — the project's front door: why it exists, the 80/20 data, the three-state contract, the report gate, the roadmap.
2. **`docs/REQUIREMENTS.md`** — the authoritative requirements and design. **The implementation follows this.** Covers the API, architecture, integration, anti-scraping operations, measured distributions, and open questions.
3. **`docs/PROVIDERS.md`** — reverse-engineering reference for each cloud drive + status-code mappings (including measured evidence: the Quark `code` table, Baidu anti-scraping, classification SQL).

Once you've read these three, you have all the background. This file only adds "the things you, as the agent, must especially keep in mind."

## Current Status
- **Documentation only, no code yet.** When you take over, the repo is a design skeleton (README + docs + go.mod + .gitignore).
- Your first task is usually **Phase 0 (skeleton) → Phase 1 (Quark + UC)**; see the README/REQUIREMENTS roadmap.
- module path: `github.com/KunMoe/kungal-link-live-checker` (Go 1.23). The stack aims for **minimal dependencies, a single binary**; use stdlib `net/http` for HTTP — don't mindlessly pull in a heavy framework.

## The Iron Rule You Must Never Break (the lifeblood of the entire project)

**Conservative three-state: `alive` / `dead` / `unknown`.**

1. **Only** when the cloud-drive API **explicitly returns "share does not exist / deleted / cancelled / expired / blocked"** may you return `dead`.
2. **Everything else** — missing passcode, rate limiting, captcha, timeout, network error, unsupported cloud drive, **any response you don't understand or have never seen** — **is always `unknown`, never `dead`.**
3. Reason: the downstream will **automatically mark a resource as dead based on `dead` alone**. The cloud-drive APIs will inevitably change; as long as you stick to "don't understand = unknown," the worst case of API drift is degrading to a manual fallback — it will **never wrongly kill a good link**. This trades "rather miss than misreport" for the downstream's unconditional trust in `dead`.
4. When writing any provider checker, first ask yourself: "Could this branch emit `dead` when I'm actually not sure?" If so, change it to `unknown`.

Breaking this rule = destroying the project's core value (eliminating false positives) with your own hands.

## Implementation Notes (pitfalls to avoid)
- **Do not scrape HTML for keywords.** The cloud drives are SPAs + anti-scraping: a bare curl to Baidu gets a straight `302`, and Quark's HTML is a fixed-length empty shell. The real status lives in their **JSON APIs**. Reverse-engineer that API for each provider.
- **Bring the passcode**: the caller will pass a `passcode` (or the URL's `?pwd=`). An encrypted share without a passcode returns "passcode required," which is `unknown`, not `dead` — always retry with the passcode first.
- **Pluggable providers**: each one implements the `Checker` interface (`Matches(url)` / `Name()` / `Check(ctx,url,passcode) → Verdict{Status,Reason,ProviderCode}`). A URL that matches no provider → `unknown / unsupported_provider`. The core **contains no downstream business logic** (failure counting / notifications / thresholds all live downstream).
- **On-demand, low-frequency, with caching and backoff.** **Never do a full crawl** (a full crawl will inevitably get your IP banned by the cloud drive). Cache `dead` for a long time, `alive` medium, `unknown` very briefly or not at all.
- **s2s authentication**, one key per consumer; **never** expose it anonymously to the public internet (otherwise it becomes a public cloud-drive probing proxy + invites bans).
- **Never store secrets in the database**: API keys / proxy credentials go through env / config files; `.gitignore` already blocks `.env*` and `config.local.*`.

## How to Verify Provider Behavior (critical)
A provider's status codes are **obtained by actual measurement**, not guessed. `docs/PROVIDERS.md` records the measured results as of 2026-06-13. You must **re-verify against the real API yourself** before hardcoding the mapping. For example, Quark (verified directly reachable):

```bash
curl -s -X POST "https://drive-pc.quark.cn/1/clouddrive/share/sharepage/token?pr=ucpro&fr=pc" \
  -H "Content-Type: application/json" -H "Referer: https://pan.quark.cn/" \
  --data '{"pwd_id":"<share_id>","passcode":"<passcode_or_empty>"}'
# code:0=alive  41008=passcode required (unknown)  41031=restricted (dead by default)  the exact code for a non-existent share is for you to confirm with a known-dead share
```

Note: ① some cloud-drive APIs behave differently for **non-mainland-China IPs**; if local re-verification looks off, first rule out geo/network factors; ② **mandatory in Phase 1**: find a share that is **definitely deleted**, confirm the exact return code for "does not exist / deleted," and add it to PROVIDERS.md — until that is confirmed, only verified codes may map to `dead`, everything else is `unknown`.

## Consumer Background (you don't have to touch it, but you should know it)
The first consumer is **kun-galgame-forum** (the forum). It wires this service in as the **gate** for "user reports a link as dead": `dead` → auto-mark dead, `alive` → reject the false report on the spot, `unknown` → fall back to its own manual mechanism. In other words, this service **only returns a verdict, it never touches downstream business**. Integration details are in REQUIREMENTS §6. In the future moyu (the patch site) will also reuse it.

## Scope Discipline (don't over-build, don't drift)
- **Eat the big chunks first**: Quark/UC (~36%, API verified) → Baidu (35.6%, the hardest, tackle it after the framework matures) → Caiyun / Xunlei / 123. The ~16% tail of magnet links / OneDrive / Mega / partner sites / image hosts is **all `unknown`**, left to downstream manual handling — **don't** go build them site-by-site from the start.
- **Complexity must be paid for by payoff**: don't prematurely adopt a proxy pool / async / Redis / background re-checking — in REQUIREMENTS §10 these are "to be decided"; add them only when there's a real need. For Phase 1, single IP + synchronous + in-memory cache is enough to get it working.
- Ambiguous design decisions (e.g. whether `41031 restricted` counts as `dead` or `unknown`, the cache backend, async) are listed in REQUIREMENTS §10 — **ask the repo owner first**, don't decide on your own.

## In One Sentence
The core value you deliver is: **for supported cloud drives, only say `dead` when you are 100% certain the link is dead; otherwise honestly say `unknown`.** Hold that line, and everything else is engineering detail.
