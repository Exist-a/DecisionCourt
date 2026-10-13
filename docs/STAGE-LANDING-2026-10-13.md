# 阶段落地记录 · 2026-10-13（信念引擎缺陷修正 + 埋点看板 + /monitor 反代上线）

| | |
|---|---|
| **日期** | 2026-10-13 |
| **上一阶段** | [`STAGE-LANDING-2026-10-12.md`](STAGE-LANDING-2026-10-12.md)（压缩 M8-2 收口 + 技术债 F1–F4） |
| **本阶段主题** | ① 信念引擎**两处缺陷**修正（真实庭审实测撞出）② 埋点 Grafana 看板（跨域关联）③ **Grafana 反代到主域 `/monitor/*` 并首次上线** ④ 修掉三个**部署脚本缺口** |
| **提交** | **5 个 commit 全部已 push 并部署**：`cdaaada` → `c178c55` → `d7ab986` → `7e19f1f` → `23be3f7` |
| **状态** | ✅ 站点在线、`/monitor` 可用、埋点看板有数据；✅ **总览看板已修（同日收尾，见 §4）** |

---

## 1. 模块清单与落地

### 1.1 信念引擎两处缺陷（`cdaaada`）

**触发**：用户在庭审页「信念轨迹」看到一条**被判为支持睡方的证据，却让睡方从 79% 掉到 74%**。

| # | 缺陷 | 根因 | 修法 |
|---|---|---|---|
| B1 | **锚定项反号** | `UpdateWithDiff` 的锚定项 `Anchor·(PriorA − prior)` 在 Agent 离自己 `PriorA` 较远时量级**盖过**证据项并把净位移反号（控方 0.79 vs 锚点 0.7：证据 +0.04 logit、锚定 −0.33 logit） | 锚定只允许**衰减**证据位移、不允许反号（异号时取纯证据项）。回归钉 `TestUpdateWithDiff_AnchorNeverReversesEvidence` |
| B2 | **零权重证据仍触发锚定** | `impact_on_option_a = impact_on_b = 0` → `w = 0` → 证据项恒为 0，但旧实现照跑锚定项，写出**标着 `source=evidence`、位移却 100% 来自锚定**的 diff（控方 0.75→0.7156，真实库 **9 条**） | `evidenceShift == 0` 时**直接跳过**。回归钉 `TestUpdateWithDiff_ZeroWeightEvidenceChangesNothing` |

> **为什么长期没被发现**：`engine_v06_test.go` 所有用例把控方初值写成 `0.70` = 其 `PriorA` → 锚定项恒为 0，**该路径零覆盖**。
> **区分度**：`w = cred·rel·|impactA|`（只与证据有关），四个 Agent 同值**不是 bug**；`BeliefDiff.Direction` 是证据级属性，四个 Agent 写同一个值。

### 1.2 埋点 Grafana 看板（`cdaaada`）

- 新增 `deploy/ops/grafana/provisioning/datasources/postgres.yml` —— Grafana **直连业务库**（uid `dc_postgres`）。
- 新增 `deploy/ops/grafana/dashboards/decisioncourt-events.json` —— **10 面板**：顶部两栏**字段说明**（事件类型含义 / 时间线各列取什么字段）+ 8 个数据面板（前端/后端事件计数、类型分布、事件速率、各阶段停留时长、**按会话的跨域关联时间线**、会话 Top 10）。
- dev/prod 两个 compose 的 grafana 服务均补 `POSTGRES_*` 透传（**prod 不补会挂一个连不上的数据源**）。

### 1.3 `/monitor` 反代上线（`cdaaada`）

- `deploy/caddy/Caddyfile`：`@monitor path /monitor/*` + `basicauth`（外层）+ `reverse_proxy grafana:3000`（**不 strip_prefix**）。
- prod grafana：`GF_SERVER_ROOT_URL=https://${DOMAIN}/monitor/` + `GF_SERVER_SERVE_FROM_SUB_PATH=true`（否则白屏）。
- `OPS_BASIC_*` 必须**透传进 caddy 容器**（否则容器读不到 → 永远 401）。

### 1.4 三个部署脚本缺口（`d7ab986` + `c178c55`）

| # | 缺口 | 后果 | 修法 |
|---|---|---|---|
| D1 | scp 只同步 `docker-compose.yml` + `deploy/caddy/Caddyfile` | `deploy/ops/**` **从未到过服务器** → Docker 把缺失的 bind 挂载源**自动建成空目录** → prometheus `not a directory` 起不来 | scp `source` 补 `deploy/ops` |
| D2 | 部署脚本只 `--force-recreate backend frontend`（+Caddyfile 变了才 caddy） | prometheus/grafana 永远停在 `Created`，`/monitor` 拿到死 upstream（502） | 部署末尾补 `docker compose up -d prometheus grafana`，**故意不致命**（`\|\|` + WARN） |
| D3 | 我给 caddy 加了 `depends_on: grafana` | `up -d --force-recreate caddy` 把三个容器建好却卡在 `Created` → 命令非零 → **CI 部署失败 + caddy 没监听 80/443 → 站点宕机约 20 分钟** | 去掉该依赖（Caddy 对未就绪 upstream 本就容忍） |

### 1.5 两处生产配置修正

| # | 问题 | 修法 | commit |
|---|---|---|---|
| E1 | Grafana 暴力破解锁把**站长本人**锁在门外（每次重试续期） | `GF_SECURITY_DISABLE_BRUTE_FORCE_LOGIN_PROTECTION=true`（理由与反向条件见 compose 注释） | `7e19f1f` |
| E2 | `OPS_METRICS_TOKEN` 只透传给了 prometheus，**backend 自己拿不到** → 生产 `/metrics` 对所有请求 401 | backend 段补 `OPS_METRICS_PUBLIC`（默认 false）+ `OPS_METRICS_TOKEN` | `23be3f7` |

### 1.6 服务器侧变更（`.env`，不入仓）

`GF_ADMIN_PASSWORD`、`OPS_BASIC_USER` / `OPS_BASIC_HASH`、`OPS_METRICS_TOKEN`、`AGENT_GATEWAY_SUMMARY_THRESHOLD=3` / `MATERIAL_KEEP_RECENT_N=6` / `LOG_RETENTION_DAYS=30`（后三个补齐了 §6.2f 列出的上线前置）。
另：`deploy/ops` 手工同步一次并 `chown` 回 `admin`（否则将来 scp 覆盖不了）。

---

## 2. 真实验证证据

### 2.1 信念引擎（`cdaaada`）

- 手推四个 Agent 后验与线上截图**精确吻合**：控方 0.7373 / 辩方 0.2728 / 调查员 0.5552 / 法官 0.5710。
- **反向验证**：禁用对应修复后，回归钉分别报 `0.7900→0.7373`、`0.2400→0.2728` 与 `0.7500→0.7156`、`0.2500→0.2844` —— 与真实库数据一致。
- `go test ./...` **退出码 0**（21 包）。

### 2.2 `/monitor`（生产）

| 检查 | 结果 |
|---|---|
| `/monitor/` 无口令 | **401** |
| 带外层口令 → Grafana 登录页 | **200**（44,923 B，含 grafana 特征） |
| `POST /monitor/login`（JSON） | **200 + 会话 cookie** |
| 带会话取看板列表 | 3 块：`DecisionCourt`(夹) / 埋点与跨域关联 / 总览 |
| `/monitor/api/health` | `{"database":"ok","version":"11.4.0"}` |
| 主站 `/health` | **200**（多次采样 7/8、11/12、5/5） |

### 2.3 埋点看板（生产逐面板查询）

**8 个面板全部返回数据**：事件总数 65（后增至 75，用户在演示中持续产生）/ 前端 42 / 后端 23 / 类型分布 10 行 / 速率 43 点 / 阶段 5 行 / 跨域时间线 22 行 / 会话 Top5。

### 2.4 部署管道

`c178c55`、`d7ab986`、`7e19f1f`、`23be3f7` 四次 **Test ✓ → Deploy ✓**；并已验证 CI **自己**同步了 `deploy/ops`（文件 mtime = 部署时刻、属主 `admin:admin`）。

---

## 3. 本阶段决策

1. **关掉 Grafana 暴力破解锁**（用户拍板）。理由：门口已有 Caddy 一道口令、管理员口令是 24 位随机串；它唯一被观察到的效果是把合法主人锁在门外。**反向条件已写进 compose 注释**：去掉外层口令或换弱口令时必须删掉该行。
2. **反代走「主域路径」而非子域**：免 DNS 变更与额外证书；代价是 Grafana 需要 `serve_from_sub_path`。
3. **外层口令 fail-closed 设计被推翻**：原设计用"已丢弃口令的哈希"做默认值防 Caddy 崩，实测更糟（静默 401 难排查）→ 改成 `OPS_BASIC_*` 用 `${:?}` **必填**，缺失时 compose 直接报错（响亮失败）。
4. **埋点看板口径**：**只读视图，不是面向用户的分析后台**，也不做漏斗聚合——被问"分析侧"仍按"未产品化"答。

---

## 4. 遗留与未解决

### ✅ 已解决 · 「DecisionCourt 总览」看板全空（2026-10-13 同日收尾）

- **现象**：Prometheus 抓取目标 `health=down`，`up` 指标**全为 0**；用户截图确认「总览」10 个面板全是 "No data"，而**另一块（埋点与跨域关联，走 Postgres）正常**。
- **根因（本轮实证，不再是"高度怀疑"）**：**Prometheus 不会在 `scrape_configs` 里做环境变量展开** —— 它只在 `external_labels` 段展开，且要 `--enable-feature=expand-external-labels` 才开。所以 `prometheus.prod.yml` 里 `authorization.credentials: ${OPS_METRICS_TOKEN}` 被当**字面量**发出。
  - **取证方法（决定性）**：起一个一次性 Prometheus 容器加载该配置，让它去抓一个"回显请求头"的临时服务 —— 抓到的是 `Authorization: Bearer ${OPS_METRICS_TOKEN}`（**未展开**）。同一方法换上"启动期 sed 展开"的入口后，抓到 `Bearer TESTTOKEN123`（真值）。
  - 与"dev 从不暴露此问题"一致：dev 走 `OPS_METRICS_PUBLIC=true` 公开通道，压根不带 token。
- **修法（已实施）**：
  1. 新增 `deploy/ops/prometheus-entrypoint.sh`（经 volumes 挂进容器），启动期把 credentials 行的占位符展开进 `/tmp` 的临时配置再 `exec prometheus`（原配置是只读挂载，不能就地改）。只替换 `credentials:` 那一行 —— 全局替换会把 token 泄露进注释文本（**实测踩到过**）。token 含 `&`/`|`/`\` 时的 sed 转义也做了（有单测级本地验证）。
  2. `docker-compose.yml` 的 prometheus 服务改 `entrypoint: ["/bin/sh","/etc/prometheus/entrypoint.sh"]` + 挂载该脚本，去掉原 `command`。
  3. `.github/workflows/deploy.yml`：prometheus 改 **`--force-recreate`**（"改了 ops 配置就一定生效"变成确定行为，而不是依赖 compose 的配置哈希检测——那失败时是**静默**的），并补一条 **抓取健康度自检**（`up` 查询，期望 `health=up`），让这类静默失败可见。
  4. `prometheus.prod.yml` 里那句错误注释（"Prometheus 启动时会做 ${VAR} 替换"）已更正。
  5. 新增根目录 `.gitattributes`（`*.sh text eol=lf`）：防止 Windows 侧写入 CRLF 导致容器内 `/bin/sh` 报错。
- **验证**：本地（不用 docker，遵用户"另一项目在用"）—— `sh -n` 通过；用**真实脚本 + 真实配置**跑三类用例：① 恶意 token（`&|` `\`）→ credentials 行逐字节等于输入、注释未被误替换、非注释行无残留占位符、YAML 可解析；② 空 token → 与源文件逐字节一致；③ credentials 行被改成引号形式 → **护栏 WARN 触发**。compose YAML 解析 + 挂载源存在性 + workflow YAML 解析 + 内嵌 shell `bash -n` 均通过。**线上验收见下方 §6**。

### 🟡 部署脚本缺口 · Grafana 环境变量变更不会自动 recreate（部分收窄）

prometheus 已改为无条件 `--force-recreate`（本轮），但 **grafana 仍是普通 `up -d`** —— 改 grafana 的 env 需手工 `docker compose up -d --force-recreate grafana`。

### 🟡 信念引擎三处遗留（未修，登记备查）

1. **锚定项在界面上不可见**：`model.BeliefSrcAnchorPull` 常量与前端 `Anchor` 图标都在，但**全仓无发射方** → 锚定被折叠进 evidence 卡片，用户会把整个 Δ 归因给证据（而锚定常是主导项）。
2. **发言（stance）触发的 diff 只写库、不广播 `belief.diff`**（只发 `belief.updated`）→ 实时视图看不到、刷新（hydrate）后突然多出来，同一份数据两副面孔；`prior` 会"凭空跳"。
3. **律师 `Anchor=0.7` 过强**：每步把"偏离先验"缩到 30%，三条证据后只剩 2.7% → 轨迹更接近"最近一条证据 + 强先验"，而非证据累积。

### 🟡 验证缺口 · 信念修复无实盘证据

B1/B2 只有**单测**（含可精确复现线上数字的回归钉），**未做浏览器实测**，也**没有一条修复后的真实证据记录**（需重新提交一条证据才会产生）。

### ⏳ 工作区两个未提交文件（非本阶段改动）

`frontend/components/courtroom/ThinkingBubble.tsx`（+127/−109，**全仓无 import 的死代码**）、`scripts/soak-local-trial.py`（+4/−1，不进镜像）。已核：对线上运行时**零影响**，`tsc --noEmit` 通过。**去留待用户决定**。

### ⏳ 运维教训（本轮踩到，值得写进规范）

1. **阿里云对密集 SSH 会 IP 级限流**，且**限流时 HTTPS 也会间歇超时**（表现"部分 200 / 部分 000"）。**判别法：同一域名下有成功响应就说明站点没挂**——本轮一度把它误判成二次宕机。缓解：把远程工作**合并成一次连接里的一个脚本**（本轮用 tar+base64 把文件塞进单次 SSH 成功）。
2. **Docker 会以 root 创建缺失的 bind 挂载源** → `admin` 删不掉，需 `docker run --rm -v ... caddy:2-alpine rmdir/chown` 以 root 处理。
3. **`git push | tail` 会吞掉退出码**（本项目已记过一次，本轮又踩）→ 断言 push 成功必须看**被测命令本身**的退出码或核对远端 SHA。

---

## 5. 关联文档

- 埋点看板与跨域关联：`docs/OBSERVABILITY.md` §8.1、`AGENTS.md` §6.2g/§11.5
- 信念引擎两处修正：`AGENTS.md` §6.2g（含三处遗留与 `air` 不热重载的运维教训）
- 部署缺口与 `/monitor`：`AGENTS.md` §6.2h、`docs/deployment/SECOND-LAUNCH-2026-10-09.md`
- 上一阶段：[`STAGE-LANDING-2026-10-12.md`](STAGE-LANDING-2026-10-12.md)

## 6. 总览看板修复 · 线上验收

> 部署后补记。

- 判据：Prometheus 抓取目标 `health=up`（`up=1`），且「总览」看板各面板出数（不再 "No data"）。
- 取证路径：CI 部署日志里的「Prometheus 抓取状态 (期望 health=up)」自检行；或经 SSH 在 `prometheus` 容器内查 `/api/v1/query?query=up`。
