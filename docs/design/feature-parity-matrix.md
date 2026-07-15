# CodeFlow 鍔熻兘瀵圭瓑鐭╅樀锛團eature Parity Matrix锛?

> 鍒涘缓锛?026-07-11  
> 鐘舵€侊細Living document  
> 瀹¤鍩虹嚎锛氫笌 `docs/plans/2026-07-11-codeflow-2.0-implementation-and-hardening-plan.md` 鍚屾  
> 鍥句緥锛氣渽 鍙敤 路 鈿狅笍 鍗婂疄鐜?路 鉂?缂哄け 路 馃攧 闇€閲嶆瀯/鍚堝苟 路 馃毇 闈炵洰鏍囷紙鏈樁娈碉級

**缁存姢绾﹀畾**锛氭瘡涓噷绋嬬缁撴潫鏇存柊銆岀姸鎬併€嶄笌銆屽娉ㄣ€嶏紱绂佹鎶婃湭钀藉湴妯″潡鏍囦负 鉁呫€?

---

## 1. 宸ョ▼涓庝富绾?

| ID | 鑳藉姏 | 鐘舵€?| 鐜扮姸鎽樿 | 鐩爣 | 閲岀▼纰?| 澶囨敞 |
|---|---|---|---|---|---|---|
| G01 | 鍗曚竴鍓嶇涓荤嚎 | 鉁?| **G01 鍚?*锛氬敮涓€涓荤嚎 `apps/workbench` / `@codeflow/workbench`锛涚墿鐞嗘棤 `apps/desktop` / `codeflow_template` | 淇濇寔鍗曟爲锛涚姝㈠啀寮曞叆绗簩鍓嶇 | M0 | **2026-07-12 G01**锛歳ename `apps/desktop`鈫抈apps/workbench`锛涘吋瀹?`dev:desktop`/`build:desktop` 鍒悕锛汵x tag `platform:desktop` 淇濈暀锛堝钩鍙拌涔夛級 |
| G02 | 鏂囨。 SSOT锛坉esign/plans/adr锛?| 鉁?| **M0.6**锛歚docs/README.md` IA锛沝esign/plans/adr/requirements锛? 浠?2.0 璁捐绾冲叆璺熻釜锛沞arly/historical 杩佸叆锛汚DR 0001/0002 | 鎸佺画鎸?IA 鍐欏叆锛涚姝㈠啀鏁ｈ惤 plan/archive | M0 | **2026-07-13**锛氳 `docs/adr/0001-docs-information-architecture.md`锛沷penapi 浠?`backend/docs/openapi.yaml` |
| G03 | 鏈煩闃垫寔缁窡韪?| 鉁?| 鏈枃妗?| 姣忛噷绋嬬鏇存柊 | M0 | |
| G30 | CGO / SQLite 鍩虹嚎鍐崇瓥 | 鉁?| **M0.9**锛氱淮鎸?`mattn/go-sqlite3` + `CGO_ENABLED=1`锛汳akefile `build-all` 瀵归綈涓?1锛沵odernc 鍚庣疆 | 杩佺Щ椤荤嫭绔?ADR/PR | M0 | `docs/adr/0003-sqlite-cgo.md` |

---

## 2. 鍓嶇澹充笌浣撻獙

| ID | 鑳藉姏 | 鐘舵€?| 鐜扮姸鎽樿 | 鐩爣 | 閲岀▼纰?| 澶囨敞 |
|---|---|---|---|---|---|---|
| G04 | 璁捐绯荤粺 + 鍩虹缁勪欢 | 鉂?| 鏃犵粺涓€ `src/ui` 浠ょ墝浣撶郴 | Tailwind 浠ょ墝 + Radix 鍩哄骇 | M1 | |
| G05 | App Shell 璺敱 IA | 鈿狅笍 | `ViewMode` 鎺у埗鍙板叓椤?| 璁捐璺敱琛?| M1 | types.ts ViewMode |
| G06 | Flow Rail + Dockview 宸ヤ綔鍙?| 鉂?| 鏃?| 闃舵鑷€傚簲甯冨眬 | M1 | |
| G07 | 鍚姩 / 鍔犺浇浣撶郴 | 鈿狅笍 | 鍩虹鍔犺浇 | 鍚姩绐?+ 楠ㄦ灦 + 澶辫触鎬?| M1 | |
| G13 | 瑙勫垝 / 鎻愪氦鐢诲竷 | 鈿狅笍 | Plan 瑙嗗浘 + workflow 瑙傛祴妯″瀷 | 闃舵鐢诲竷 | M2 | adapters/workflows 鍙鐢?|
| G15 | 缂栫爜鐢诲竷 | 鉂?| 鏃?Monaco 宸ヤ綔鍙?| stages/coding | M3 | |
| G18 | 鎯虫硶 / 璁捐 / Review 鐢诲竷 | 鉂?| 鏃?| stages/* | M4 | |
| G21-UI | 璋冪爺 / 瀵煎叆鐞嗚В鐢诲竷 | 鉂?| 鏃?| DeepSearch + comprehension | M5 | |
| G22-UI | 閰嶇疆涓績鍥涘瓙鍖?| 鈿狅笍 | Settings 鏁ｈ惤 | 娓犻亾/妯″瀷/MCP/Skill | M5 | |
| G24 | Live Preview + 妫€鏌ュ櫒妗?| 鉂?| 鏃?| Tauri WebView / iframe | M6 | |
| G25 | 缃戦〉鍝嶅簲寮?+ 鎵嬫満浼翠荆 | 鉂?| 鏃?mobile app | embed 鍝嶅簲寮?+ apps/mobile | M7 | |

---

## 3. 娴佺▼涓庡揩鐓?

| ID | 鑳藉姏 | 鐘舵€?| 鐜扮姸鎽樿 | 鐩爣 | 閲岀▼纰?| 澶囨敞 |
|---|---|---|---|---|---|---|
| G08 | Snapshot 鐪?restore | 鈿狅笍 | conversation/vector/graph **鐪?restore**锛圥R-4/5锛夛紱git hard reset 浠?opt-in | 浜у搧鍖?API + 鍙楁帶 git + 鎵ц閿?| M2 鍓嶇疆 | experimental 璀﹀憡宸叉洿鏂?|
| G09 | Flow 鎵ц寮曟搸 | 鈿狅笍 | 鐘舵€佹満 + SQLite + abort/gate decide + timeline/overview 妗?| WS flow.* + 妯℃澘甯傚満 | M2 | `internal/floweng` |
| G10 | 闃舵鑷姩蹇収涓庡洖璺?| 鈿狅笍 | advance 鍙寕 snapshot hook锛沴oop 鍥炶烦瀛樺湪锛涙湭缁戝叏閲?restore UX | loop 鈫?snapshot restore 闂幆 | M2 | |
| G11 | Artifact / Gate 涓€绛夊叕姘?| 鈿狅笍 | 妯″瀷 + gate decide API + artifact stale on loop | 瀹屾暣 CRUD/瀹℃壒 UI | M2 | |
| G12 | 宸ヤ綔娴佹ā鏉垮彲瑙嗗寲缂栬緫鍣?| 鉂?| 鏃?| 鑺傜偣鐢诲竷 JSON 瀵煎叆瀵煎嚭 | M2 | |
| G14 | workflow 瑙傛祴涓庡紩鎿庡悎涓€ | 鈿狅笍 | timeline **宸插悎骞?* floweng 浜嬩欢锛坙ane=floweng锛夛紱overview/replay 浠?planner/audit 鎷艰 | overview 鍚?Flow 鐘舵€侊紱replay 娑堣垂 flow events | M2 | 2026-07-15 bridge |

---

## 4. Agent銆佽京璁恒€佸畧鍗?

| ID | 鑳藉姏 | 鐘舵€?| 鐜扮姸鎽樿 | 鐩爣 | 閲岀▼纰?| 澶囨敞 |
|---|---|---|---|---|---|---|
| G19 | 澶氭柟澶氭ā鍨嬭京璁?| 鈿狅笍 | Generator/Critic(+Mediator)锛?*flow_id/stage_id FK**锛涘唴瀛?| 2~N + model/channel | M4 | `internal/debate` |
| G20 | Agent 骞垮満 / Registry | 鈿狅笍 | agent 鏈嶅姟鍩虹鑳藉姏 | 鐗堟湰/鏉ユ簮/鎻掓Ы/甯傚満 UI | M4 | |
| G16 | `internal/workspace` | 鈿狅笍 | list/read/write + 娌欑 + **stage/promote**锛汚PI experimental锛涙棤 watch/WS | watch + project root 缁戝畾 | M3 | 2026-07-15 |
| G17 | `internal/guard` | 鈿狅笍 | WriteGuard + AST 閲嶅妫€娴?+ guard.yaml + IndexTree + audit bridge + check/index API | 璞佸厤瀹℃壒 + 鍏ㄩ噺 shadow 娴佹按绾?| M3 | 2026-07-15 |
| 鈥?| 鍙屾柟杈╄ API | 鉁?| create/round/resolve/export/stream | 淇濈暀骞跺崌绾?| M4 | 涓嶅洖閫€鐜版湁 API 鐩磋嚦鍏煎灞?|

---

## 5. 璁板繂銆佹绱€侀厤缃€佹彃浠?

| ID | 鑳藉姏 | 鐘舵€?| 鐜扮姸鎽樿 | 鐩爣 | 閲岀▼纰?| 澶囨敞 |
|---|---|---|---|---|---|---|
| 鈥?| 鍙岃建璁板繂 / SAMG 鍩虹 | 鉁?| memory + samg | 缁х画澧炲己 | 璐┛ | 蹇収 capture 宸茬敤 |
| 鈥?| 閰嶇疆绯荤粺 / 鐑垏鎹?| 鉁?| config + hotswap | 閰嶇疆涓績 UI 澧炲己 | M5 | |
| 鈥?| Hook / 瀹¤ / 闅愮 / 鎶湶 | 鉁?| 搴曞骇瀛樺湪 | 涓?gate/guard 涓茶仈 | 璐┛ | |
| 鈥?| 榛戞澘 / 鎸囨尌瀹?| 鉁?| 瀛樺湪 | 鎸傛帴闃舵 | 璐┛ | |
| G21 | DeepSearch + 瀵煎叆娴佹按绾?| 鈿狅笍 | search/retriever 搴曞骇 | 鑱旂綉 Provider + 瀵煎叆娴?| M5 | |
| G22 | Skill 璧勪骇鏈嶅姟 | 鈿狅笍 | Registry CRUD/Match/Inject + **SQLite 鎸佷箙鍖?*锛坄skills.db`锛夛紱2 builtin锛汚PI experimental | frontmatter 甯傚満 + Agent 鎸傝浇 UI | M5 | 2026-07-15 |
| G23 | 鎻掍欢璐＄尞鐐?+ 娌欑 | 鈿狅笍 | plugin + isolation 閮ㄥ垎 | 娉ㄥ唽琛ㄤ笌鏇挎崲鐐?| M6 | |

---

## 6. 鏋舵瀯鍗敓

| ID | 鑳藉姏 | 鐘舵€?| 鐜扮姸鎽樿 | 鐩爣 | 閲岀▼纰?| 澶囨敞 |
|---|---|---|---|---|---|---|
| G26 | DI 鍘婚櫎鍏ㄥ眬 Get/Set | 鈿狅笍 | bootstrap B0+B1 鍏湇鍔?Apply锛沨andlers 浠嶈蛋 Get* | 鎸夊煙缁х画 B2+ 鍒犻櫎鍏ㄥ眬 | 璐┛ | **2026-07-15 PR-6**锛歋napshot/Debate/Summarize 宸插叆 bootstrap |
| G27 | summarize 鍚堝苟 | 鉁?| **M0.8**锛氫粎 `internal/summarize`锛沞ngine锛圕ompressor/TokenCounter锛夎縼鍏ワ紱鍒犻櫎 `internal/summarizer` | 淇濇寔鍗曞寘锛汚PI 闈笉鍙?| M0 | **2026-07-15**锛歨andlers/OpenAPI 浠?`/api/v1/summarize`锛汦ntitySkeleton 涓?API DecisionSkeleton 鍒嗗瀷 |
| G28 | Schema-first OpenAPI | 鈿狅笍 | 鏈?TS 鐢熸垚鑴氭湰 | YAML SSOT + CI | 璐┛ | 杈撳嚭 `apps/workbench/generated/`锛涘绾?`backend/docs/openapi.yaml` |
| G29 | WS 缁熶竴浜嬩欢鎬荤嚎 | 鈿狅笍 | hub 瀛樺湪锛?*flow_event 骞挎挱**锛沝ebate stream 浠嶇嫭绔?| 鍗曡繛鎺ュ璺?topics | M2鈥揗3 | 2026-07-15 floweng鈫扺S |
| G31 | 浠撳簱鐢熸垚鐗?hygiene | 鉁?| untrack node_modules锛沢itignore 寮哄寲锛汣I Repo Hygiene Guard | 鎸佺画绂佹 tracked 鐢熸垚鐗?渚濊禆 | M0 | M0.5锛歚scripts/check-repo-hygiene.mjs` + `pnpm check:repo-hygiene` |

---

## 7. 鍚庣 API 闈紙鎽樿锛?

| 璺敱鏃?| 鐘舵€?| 璇存槑 |
|---|---|---|
| `/api/v1/snapshots` | 鈿狅笍 Experimental | 鐪?restore锛坈onv/vector/graph锛夛紱git opt-in |
| `/api/v1/workflows/:projectId/*` | 鈿狅笍 瑙傛祴 | overview/timeline/replay锛泃imeline/summary 鍚?floweng |
| `/api/v1/debates` | 鈿狅笍 鍙屾柟 | 鏈粦 Flow stage |
| `/api/v1/flows` | 鈿狅笍 Experimental | create/advance/skip/loop/abort/gate decide |
| `/api/v1/workspace` | 鈿狅笍 Experimental | list/read/write/promote |
| `/api/v1/skills` | 鈿狅笍 Experimental | CRUD/match/inject/import |
| `/api/v1/guard` | 鈿狅笍 Experimental | check/index |
| 闈欐€?embed `/` | 鉁?| `static.go` + dist锛堥渶鏋勫缓鍚屾锛?|

---

## 8. 鏇存柊鏃ュ織

| 鏃ユ湡 | 鍙樻洿 |
|---|---|
| 2026-07-11 | 鍒濈増锛氬榻?2.0 瀹炴柦璁″垝 搂3 宸窛鐭╅樀 |
| 2026-07-11 | G01 澶囨敞琛ュ己锛欰pp.tsx 鍚?SHA + Nx project.json 閿欒缁戝畾璇佹嵁锛堜笌 2.0 璁″垝绗簩杞鏍稿榻愶級 |
| 2026-07-11 | **PR-2**锛氬伐鍏烽摼鏀圭粦 `apps/desktop`锛汫01 澶囨敞鏇存柊锛堝弻鏍戞畫鐣?鈫?浠?鈿狅笍锛屽緟 PR-3锛?|
| 2026-07-12 | **PR-3**锛氬垹闄?`codeflow_template`锛汫01 鐜扮姸鏀逛负鐗╃悊鍙屾爲宸插垹锛屼粛 鈿狅笍 鑷?rename workbench |
| 2026-07-12 | **G01**锛歳ename `apps/desktop`鈫抈apps/workbench`锛汫01 鈫?鉁?|
| 2026-07-13 | **M0.5**锛欸31 浠撳簱鐢熸垚鐗?hygiene 鉁咃紱untrack node_modules锛汣I Repo Hygiene Guard |
| 2026-07-13 | **M0.6**锛欸02 鏂囨。 SSOT 鉁咃紱docs IA + ADR 0001/0002锛?.0 璁捐浜斾欢濂楃撼鍏ヨ窡韪紱early/plan/backend 鏁ｆ枃鏀舵暃 |
| 2026-07-15 | **M0.8**锛欸27 summarize 鍚堝苟 鉁咃紱鍒犻櫎 `internal/summarizer`锛沞ngine 骞跺叆 `internal/summarize` |
| 2026-07-15 | **M0.9**锛欸30 CGO/SQLite ADR 鉁咃紱`docs/adr/0003-sqlite-cgo.md`锛汳akefile `build-all` CGO=1 |
| 2026-07-15 | **PR-6 鏀跺熬**锛歜ootstrap 娉ㄥ叆 Snapshot/Debate/Summarize锛汫26 澶囨敞鏇存柊 |
| 2026-07-15 | **PR-8**锛歚internal/floweng` 鏈€灏忔満 + experimental flows API锛汫09 鈫?鈿狅笍 |
| 2026-07-15 | **M3.1**锛歚internal/workspace` list/read/write + WriteGuard 閽╁瓙锛汫16 鈫?鈿狅笍 |
| 2026-07-15 | **M3.2**锛歚internal/guard` 寮曟搸 + workspace 寮哄埗鎸傞挬锛汫17 鈫?鈿狅笍 |
| 2026-07-15 | **M5.0**锛歚internal/skill` registry + match/inject API锛汫22 鈫?鈿狅笍 |
| 2026-07-15 | **G14 bridge**锛歸orkflow timeline 鍚堝苟 floweng events |
| 2026-07-15 | **floweng SQLite**锛欶lowStore + SQLiteFlowStore锛沵ain 榛樿 `data/floweng.db` |
| 2026-07-15 | **guard AST**锛歋ymbolIndex 璺ㄦ枃浠?duplicate_symbol 瑙勫垯 |
| 2026-07-15 | **skill SQLite**锛歚NewSQLiteRegistry`锛沵ain 鈫?`data/skills.db` |
| 2026-07-15 | **guard.yaml + IndexTree + audit bridge**锛沵ain 鎸?audit锛泂kill frontmatter锛沷penapi flows/workspace/skills |
| 2026-07-15 | workspace staging/promote; floweng abort + SQLite锛泂kill import dir锛沢uard /check /index锛沘udit files锛沷verview flow counts |
