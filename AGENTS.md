# Agent 行为规范

本文档用于约束 Agent 在 DecisionCourt 项目中的行为规范，确保开发过程的规范性和一致性。

## 1. 问题处理规范

### 1.1 对照文档
遇到问题时，Agent 必须：
- 首先查阅项目相关文档（位于 `docs/` 目录）
- 对照 API 设计文档 (`decisioncourt-api-design.md`) 确认接口规范
- 对照数据库设计文档 (`decisioncourt-db-design.md`) 确认数据结构
- 对照技术规范文档 (`decisioncourt-tech-spec.md`) 确认技术实现
- 对照 PRD 文档 (`decisioncourt-prd.md`) 确认业务需求

### 1.2 文档一致性
修改项目时，Agent 必须：
- 明确修改意图后，同步更新对应的文档内容
- 确保 API 变更在 API 设计文档中有记录
- 确保数据模型变更在数据库设计文档中有记录
- 确保业务逻辑变更在 PRD 或技术规范文档中有记录
- 文档更新必须清晰说明变更原因和影响范围

## 2. 裁决类型处理规范

### 2.1 先讨论后执行
遇到裁决类型的需求时，Agent 必须：
1. **禁止直接执行** - 不得在未讨论的情况下直接实现裁决相关功能
2. **主动发起讨论** - 向用户说明裁决场景的具体需求
3. **明确裁决逻辑** - 与用户确认裁决的触发条件、评判标准、输出格式
4. **获得明确授权** - 在用户明确同意后方可开始实现

### 2.2 裁决场景识别
以下场景属于裁决类型，需要先讨论后执行：
- 法官判决逻辑的实现
- 证据有效性判定
- 争议焦点裁决
- 辩论结果评判
- 任何涉及最终决策判断的功能

## 3. 测试维护规范

### 3.1 同步更新测试
修改完成后，如果相关代码存在测试，Agent 必须：
- 检查现有测试文件（`*_test.go` 文件）
- 根据代码变更更新测试用例
- 确保测试覆盖率不降低
- 运行测试验证修改的正确性
- **严禁简化测试** - 不得因为测试不通过而简化或删除测试用例，必须修复代码以通过测试

### 3.2 测试文件位置
- 后端测试：与源文件同目录，命名格式 `xxx_test.go`
- 测试输出文件：位于 `backend/test-output/` 目录
- 集成测试：标注 `_integration_test.go` 后缀

## 4. 错误处理规范

### 4.1 问题升级机制
遇到问题连续两次未能解决时，Agent 必须：
1. **主动上报** - 向用户明确说明当前问题及已尝试的解决方案
2. **添加诊断日志** - 在关键位置添加详细的调试日志
3. **请求协助** - 寻求用户提供更多信息或调整方向

### 4.2 日志添加规范
添加的日志应包含：
- 问题发生的时间点
- 相关输入参数和状态
- 错误信息或异常状态
- 执行路径和关键决策点
- 日志级别应使用适当的级别（DEBUG/INFO/WARN/ERROR）

## 5. 代码修改规范

### 5.1 修改前准备
- 理解现有代码逻辑
- 评估修改的影响范围
- 确认修改符合设计文档

### 5.2 修改过程
- 保持代码风格一致性
- 不引入不必要的复杂度
- 及时更新相关文档和测试

### 5.3 修改后验证
- 运行相关测试确保功能正确
- 检查是否有遗漏的文档更新
- 验证修改是否影响其他模块

## 6. 文档清单

Agent 需要熟悉以下核心文档：

### 6.1 主项目文档（`docs/` 目录）
- `decisioncourt-prd.md` - 产品需求文档
- `decisioncourt-api-design.md` - API 接口设计
- `decisioncourt-db-design.md` - 数据库设计
- `decisioncourt-tech-spec.md` - 技术规范
- `decisioncourt-agent-design.md` - Agent 设计文档
- `decisioncourt-roadmap.md` - 项目路线图
- `decisioncourt-ux-refinement.md` - UX 细节规范
- `project-ideas.md` - 项目灵感池

### 6.2 已完成的进行中设计文档（`docs/archive/` 目录）

以下文档已完成实施并归档到 `docs/archive/`，作为历史决策记录：

- `memory-a2a-redesign.md` — **v0.5 记忆系统 + A2A 重设计**（Episodic Memory via A2A、ContextView 投影、前端 MemoryAuditPanel、SessionUUID 房间钥匙 bug 修复、MemoryEntry 结构化字段）。最终归档版本 `memory-a2a-redesign-v1.2.md`（2026-07-01），所有 PR 已完成。
- `todolist1-pr1-contextview.md` — PR 1 ContextView 投影详细规划
- `庭审可视化简化计划.md` — 庭审页面视觉简化
- `质证阶段轮次控制修改计划.md` — cross-exam 阶段轮次控制
- `ecs-end-of-life-2026-08-05.md` — ECS 基础设施不续购决策（项目继续个人长期维护）
- `security-audit-2026-07-03.md` — v0.8.3 OWASP Top 10 安全审计报告（所有 20 项 P0/P1/P2/P3 已修复）

### 6.2b 当前进行中的设计文档（`.trae/documents/` 目录）

`.trae/` 目录被 `.gitignore` 排除，作为本地进行中设计稿存放区（不入仓）。当前活跃：

- `silent-error-fix-plan.md` — 静默错误全局修复方案（v1.1, 2026-08-05 收尾：PR 1-5/7 已合入 v0.10.17, 剩余 3 项入 `docs/todo/deferred-items-2026-08-05.md` D2, 详见 ADR 0024。**2026-08-20 v1.0.0 状态更新**：D2 全部 3 项已在 v0.10.25 (PR 079371d + c8d76dc) 完成，12/12 黑洞 100% 修复。**2026-09-21 v2.6 状态更新**：`docs/todo/deferred-items-2026-08-21.md` §D2 (cross-exam content silent error) + §D3 (direct_verdict fallback round=0) 已收尾（ADR 0040 + 9 sub-test），剩余 `streamSpeakContent` 流式解析根因 + `JudgeFinalDecision`/`GenerateVerdict` retry-on-canceled 待下次 PR）
- `security-audit-2026-07-03.md` — 安全审计 v1.0（2026-07-03 全部 20 项 P0/P1/P2/P3 已修复或 deferred，P0-1 ~ P0-6 在 v0.8.3 + v0.10.18 全部完成，P1-P3 14 项 deferred D1）

> **注意**：已完成的进行中设计文档（即 `docs/archive/` 下文件）的元数据在 AGENTS.md / README / 文档交叉引用中仍可能存在，但**实际文档位置**以本节为准。修改相关代码前应优先参考 `docs/` 主项目文档而非 archive 中的历史计划。

### 6.2c v1.0.0 发版文档（2026-08-20 新增）

- `docs/release-notes/v1.0.0.md` — v1.0.0 发版说明（ECS 30 天沉淀收尾 + DeepSeek v4 迁移 + 8 问题专项回归测试护栏，14 章节模板）
- `docs/V1-ROADMAP.md` — v1 → v2 路线图（M0 ✅ / M1 候选 4 / M2 商业化 / M3 安全 P1 / M4 v2.0）
- `docs/adr/0029-deepseek-v4-migration.md` — DeepSeek v3→v4 模型硬迁移决策

### 6.2d v2.0 REDESIGN PIVOT 历史（2026-08-22 → 23，**已完全回退**）

> **⚠️ 关键历史**：v2.0 REDESIGN 尝试 3D r3f 重构（11 个 commit 累计 9 commit 视觉试错失败），用户决定放弃 3D 路线，**git reset --hard 5fd803b 完全回退**。当前项目状态是 v1.0.4 PR-C4，v2.0 原始剪影方案保留。

**相关文档**（全部为历史/归档）：
- [`docs/postmortem/v2.0-redesign-3d-pivot.md`](postmortem/v2.0-redesign-3d-pivot.md) — **完整复盘**（5 Whys 根因 + 教训 + 行动项）— 新代码改动前必读
- [`docs/V2.0-REDESIGN-PLAN.md`](V2.0-REDESIGN-PLAN.md) — 3D r3f 重构规划（**已 SUPERSEDED**）
- [`docs/v2-redesign/`](v2-redesign/) — 阶段 1-4 详细文档（**全部 SUPERSEDED**）
- [`docs/adr/0034-archive-3d-pivot.md`](adr/0034-archive-3d-pivot.md) — 三次决策链终态（剪影 → r3f → 二次归档）
- [`docs/adr/0034-supersede-2-5d-r3f.md`](adr/0034-supersede-2-5d-r3f.md) — supersede 原版（状态 ⚠️ Archived by 0034-archive）

**给未来 Agent 的新规则**（从 postmortem §5 提炼）：
1. **能力边界前置评估**——超出 Agent 能力的任务（3D / 美术 / 真实照片），**不开始**
2. **3 次失败换方向**——同一任务连续 ≥3 次用户反馈负面，**主动报告**"需要换方向"，不"再调一下"
3. **抽象 > 模拟**——法庭的"语义"（天平 / 金色 / 对比）能用 CSS 表达，不需要 3D 物理真实
4. **"用户给参考图"是视觉重做的正确路径**——不"自由发挥"

## 7. 禁止事项

Agent 在工作过程中禁止：
- 在未查阅文档的情况下凭记忆或假设进行修改
- 在未讨论的情况下直接实现裁决逻辑
- 修改代码而不更新相关文档
- 修改代码而不更新相关测试
- 因测试不通过而简化或删除测试用例
- 忽视连续失败的问题而不上报

## 8. 敏感文件红线（SECRET_FILE_POLICY · 2026-07-02 增补）

### 8.1 触发背景

2026-07-02 v0.8 白盒化 demo 时，Agent 用"上次 Read 到的内容"作为 `old_str` 删除 `.env` 注释，但实际文件已被用户**手动填回**了真实 API key。结果 SearchReplace 工具**把 key 当成"待删内容"清空了**，导致两次需要用户重新填 key。

**根因**：`.env` 类敏感文件 + 工具的 `old_str` 替换机制 + Agent 上下文里的"过期内容" = 高危组合。即使 Agent 知道"不要碰 .env"，单次失误成本极高（key 报废、依赖停服）。

### 8.2 红线规则

Agent 在任何场景下**禁止**对以下路径执行写操作（`Edit` / `SearchReplace` / `Write` / `DeleteFile` / `RunCommand` 含 `>` / `tee` / `echo` 等任何会改变文件内容的命令）：

| 路径模式 | 原因 |
|---|---|
| `.env` | 业务 API key（LLM / 搜索 / DB） |
| `.env.*`（.env.local / .env.production / .env.development 等） | 同上 |
| `**/credentials*` / `**/secrets*` / `**/*.pem` / `**/*.key` | 凭证 / 私钥 |
| `**/id_rsa*` / `**/.ssh/**` | SSH 密钥 |
| `**/google-credentials.json` / `**/service-account*.json` | 云厂商凭证 |
| 用户在对话中**明确点名不要碰**的任何路径 | 用户主权 |

**`Read` 工具可以用**（只读不写），但 Agent 必须：
- 不得将读取到的 key 值写入**任何其他文件**（包括 demo 脚本 / 测试 fixture / 文档示例）
- 不得将 key 值回显到对话（用 `sk-***` 代替）
- 如需在命令中使用 key，**优先用临时环境变量**（`$env:FOO='bar'`），不写文件

### 8.3 替代方案（用户场景下的执行方式）

| 场景 | 推荐做法 |
|---|---|
| 需要切换 search provider（mock ↔ searxng ↔ bocha） | 启动 backend 时用 **PowerShell 临时环境变量**覆盖（`$env:SEARCH_PROVIDER='bocha'`），不改 .env |
| 需要测试不同 LLM key | 同上，用 `$env:LLM_API_KEY=...` 临时覆盖 |
| 需要确认 .env 内容 | `Read` 工具只读，不修改 |
| 需要新增配置项 | **修改 config.go 的 viper.SetDefault + 添加 .env.example**，由用户手工同步 .env |

### 8.4 违反后果

Agent 违反本规则导致 `.env` key 被清空 / 覆盖 / 泄露：
- **立即停止当前任务**，主动告知用户
- 不得尝试"自己恢复"（Agent 不知道原 key）
- 等待用户手工恢复 + 评估影响范围

### 8.5 例外

唯一例外：用户**显式、明确、单独说一次**"改 .env 第 X 行"——但 Agent 仍需在执行前用 `Read` 工具读当前完整内容，再用读到的内容做精确 `old_str`。**任何模糊指令（如"删注释"、"改配置"）一律视为不授权**。

---

## 9. ECS 运维连接能力（OPS_CONNECTION_POLICY · 2026-07-12 增补）

### 9.1 触发背景

2026-07-12 v0.10.18 部署失败（Deploy to ECS job 失败 2 次）时，Agent 因**默认不知道能 SSH 到 ECS**而让用户手动跑命令查 docker logs，导致诊断延迟。

**事实**：本机 `~/.ssh/id_ed25519` + `~/.ssh/id_rsa` 是 ECS 部署密钥（与 GitHub Actions Secrets `ECS_SSH_KEY` 同源）。Agent 通过 `RunCommand` 调 `ssh` 命令即可直连 ECS，不需要 user 中转。

**根因**：AGENTS.md 没写这条能力 → Agent 每次遇到"线上问题"都默认 user 跑命令 → 浪费用户时间。

### 9.2 ECS 连接信息（user 提供，2026-07-12 起；2026-08-05 ECS 终止后作为历史参考保留）

> **状态更新（2026-08-05）**：ECS `47.239.152.177` 已停止续购，到期后自动释放。本节 SSH 连接信息保留供将来部署到**自有云环境**时参考。§9.3 / §9.4 / §9.5 的 SSH 操作模板仍适用于任何 ECS 实例，仅 ECS_HOST / ECS_USER 需替换为新环境。

**存储位置**：`secrets/ecs.env`（gitignored，仓库不追踪）

| 项 | 值 |
|---|---|
| **ECS_HOST** | `secrets/ecs.env::ECS_HOST`（user 提供，2026-07-12） |
| **ECS_USER** | `secrets/ecs.env::ECS_USER`（v0.10.15 deploy.yml Secrets 修复后确认） |
| **SSH_KEY** | `secrets/ecs.env::ECS_SSH_KEY_PATH`（本地路径，与 GitHub Secrets `ECS_SSH_KEY` 同源 ed25519 key） |
| **ECS 项目目录** | `secrets/ecs.env::ECS_PROJECT_DIR`（v0.10.12 修过大小写） |
| **Docker Compose** | `docker compose`（v2 CLI，`docker-compose` v1 已废弃） |

**使用方式**：Agent 每次 SSH 前 `Read secrets/ecs.env` 拿最新值（避免硬编码）。也可以用 `Get-Content secrets/ecs.env | ForEach-Object { if ($_ -match '^([^#][^=]+)=(.*)$') { Set-Item -Path "Env:$($Matches[1])" -Value $Matches[2] } }` 注入到 PowerShell 环境变量。

**为什么 gitignored**：`secrets/` 已在 `.gitignore`（§8.1 提到的"Local secrets backups"分类）。ECS IP 不算高敏，但公开给攻击者多一个扫描目标。

### 9.3 允许 Agent 直接执行的 SSH 操作

| 操作 | 命令模板 | 适用场景 |
|------|----------|----------|
| 查看容器状态 | `ssh -i $env:USERPROFILE\.ssh\id_ed25519 admin@<ECS_HOST> "cd /opt/DecisionCourt && docker compose ps"` | Deploy 失败 / 容器 crash |
| 查看 backend 日志 | `ssh ... "cd /opt/DecisionCourt && docker compose logs --tail=50 backend"` | 查 fail-fast 原因 |
| 查看 frontend 日志 | `ssh ... "cd /opt/DecisionCourt && docker compose logs --tail=30 frontend"` | 前端异常 |
| 查看 host .env 关键项 | `ssh ... "cd /opt/DecisionCourt && grep -E '^(JWT_SECRET\|DATABASE_URL\|LLM_API_KEY)' .env \| sed 's/=.*/=<hidden>/'"` | 确认 fail-fast 诱因 |
| 查看 VOLUME 权限 | `ssh ... "ls -ld /opt/DecisionCourt/logs /opt/DecisionCourt/logs/backend"` | UID 10001 权限 |
| 看镜像 tag | `ssh ... "docker images \| grep decision-court"` | 镜像是否拉下来 |
| 触发手动 deploy | `ssh ... "cd /opt/DecisionCourt && ./deploy-on-ecs.sh"` | Deploy workflow 失败后手动重跑 |
| 健康检查 | `ssh ... "cd /opt/DecisionCourt && docker compose exec -T backend wget -qO- http://127.0.0.1:8080/health"` | 容器是否真健康 |

### 9.4 禁止 Agent 直接执行的 SSH 操作

| 操作 | 原因 |
|------|------|
| `docker compose down` / `docker rm` / `rm -rf` | 销毁性操作，需 user 显式授权 |
| 修改 `/opt/DecisionCourt/.env` | 包含真实 key，违反 §8 敏感文件红线 |
| `docker compose up -d` 不带 `--force-recreate` | 可能复用旧容器，新镜像不生效 |
| `kill -9` / `pkill -f` 任何进程 | 绕过 docker 生命周期管理 |
| `chmod 777` / `chown -R` 大范围改权限 | 安全降级 |

### 9.5 最佳实践

1. **诊断顺序**：先用 §9.3 表里的"轻量命令"（`docker compose ps` + `logs --tail=50`），90% 问题一次定位
2. **不要乱试修复命令**：找到 root cause 后，**先告诉用户修复方案**，让 user 决定要不要执行（或 user 明确授权 Agent 执行）
3. **不回显 key**：如 grep 出 `JWT_SECRET=...`，用 `sed 's/=.*/=<hidden>/'` 隐藏
4. **超时保护**：用 `command_timeout: 30s` 或 `-o ConnectTimeout=10` 防止 hang
5. **输出截断**：用 `| head -n 100` / `| tail -n 50` 限制输出大小

### 9.6 异常处理

| 现象 | 排查方向 |
|------|----------|
| `Permission denied (publickey)` | SSH_KEY 路径错 / key 失效 / known_hosts 不一致 |
| `Connection timed out` | ECS 安全组未放行本地 IP / ECS 没开机 |
| `Host key verification failed` | `ssh-keyscan -t ed25519 <ECS_HOST>` 更新 known_hosts |
| `bash: command not found` | ECS 上没装该命令（如 `jq` / `htop`）|

---

## 10. ECS 连接信息更新记录

| 日期 | ECS_HOST | ECS_USER | SSH_KEY | 备注 |
|---|---|---|---|---|
| 2026-07-12 | _待 user 提供_ | — | — | v0.10.18 Deploy 失败时建立本节 |
| 2026-08-05 | `47.239.152.177` | `admin` | `~/.ssh/id_rsa` | **本节正式填充**。30 天生产沉淀 + 备份验证时发现：`id_ed25519` Permission denied，`id_rsa` 可用；同步修正 `secrets/ecs.env` + 加此行记录。配套：[`docs/archive/ecs-end-of-life-2026-08-05.md`](docs/archive/ecs-end-of-life-2026-08-05.md) + [`docs/deployment/_archived/production-retrospective-2026-08-05.md`](docs/deployment/_archived/production-retrospective-2026-08-05.md)。**2026-08-05 用户决策不续购 ECS，但 SSH_KEY 信息保留供将来部署到自有云时复用；本表无新增行** |

---

## 11. Docker 业务测试规范（DOCKER_TEST_POLICY · 2026-09-14 增补）

### 11.1 触发背景

2026-09-14 v2.1 F5 收尾时，Agent 仅做了**静态冒烟**（源码默认值检查 + `TestF5DefaultSwitchesTrue` / `TestF5DefaultsRespectedFromEnv` 两个测试 PASS + 跑 `go test ./...` 全量），**没有跑真实业务链路**：

- 没启动 backend → 没确认 `/api/v1/health/llm` 实际返回什么
- 没启动 frontend → 没确认 LLM banner 在浏览器里显示什么样
- 没创建 trial → 没确认 `AGENT_GATEWAY_SMART_COMPRESSION=true` 默认值下，长庭审 token 是不是真降了
- 没看 `/api/v1/metrics` → 没验证 `agent_gateway_llm_total_tokens_per_call` 在 500-2000 健康范围

**根因**：AGENTS.md §3 只规定「跑 `*_test.go`」和 §9 §5「soak 脚本占位」，没明确说"**业务功能验证必须启动 docker**"。Agent 倾向"测试过了就行"——但单元测试覆盖的是内部约定，**业务集成（容器间网络 / host ↔ container / 真实 LLM 调用）必须靠 docker 启动验证**。

**事实**：本机 Docker Desktop 可用，`docker-compose.dev.yml` 是已实装的开发栈（postgres + redis + backend + frontend，源码 bind mount，HMR 即时生效）。Agent 启动 dev compose 即可**完整跑业务链路**，不需要 user 中转。

### 11.2 触发条件（什么时候必须启动 docker 测）

以下场景，Agent **必须**启动 dev compose 跑业务验证（不能只跑单元测试就交差）：

| 场景 | 原因 |
|---|---|
| 修改 `docker-compose*.yml` / `Dockerfile*` | 容器编排变更直接影响启动 |
| 修改 `config.go` 默认值（v2.1 F5 教训） | env 优先级 / viper.SetDefault / container env 注入路径是经典踩坑点 |
| 修改 `/api/v1/health/*` / 启动检测逻辑（v2.1 F4 教训） | fail-fast 行为只能通过真实启动看到 |
| 修改 WebSocket / courtroom 状态机 | 后端 ↔ 前端实时链路，单测覆盖有限 |
| 修改 LLM Gateway / Agent Runner / Prompt Lab | 涉及真实 LLM 调用，单测都用 mock |
| 修改 frontend `NEXT_PUBLIC_*` | 构建期注入，运行时改无效 |
| 修改 `Caddyfile` / 反代 / 路由 | 反代层在容器外 |
| 任何 release notes / PR-4 阶段（用户授权打 tag 之前） | 防止"测试过但用户跑不起来" |

**反例**（**不**需要 docker）：
- 纯函数 / 工具方法 / variants / 动画参数
- 前端组件 props / 视觉微调（能用浏览器 HMR 看的）
- 后端类型 / 接口定义 / ORM model
- 单测覆盖 ≥ 80% 且不涉及外部依赖的逻辑

### 11.3 允许 Agent 直接执行的 Docker 操作

| 操作 | 命令模板 | 适用场景 |
|------|----------|----------|
| 启动 dev 栈（首次/重建） | `docker compose -f docker-compose.dev.yml up -d --build` | 任何 docker 业务测试的起点 |
| 启动 dev 栈（增量） | `docker compose -f docker-compose.dev.yml up -d` | 改完代码 HMR 自动生效 |
| 查看容器状态 | `docker compose -f docker-compose.dev.yml ps` | 启动后看 4 个容器都 healthy |
| 查看 backend 日志 | `docker compose -f docker-compose.dev.yml logs --tail=50 backend` | 调试启动 / fail-fast |
| 查看 frontend 日志 | `docker compose -f docker-compose.dev.yml logs --tail=30 frontend` | 浏览器看到异常时 |
| 查看 postgres 日志 | `docker compose -f docker-compose.dev.yml logs --tail=20 postgres` | DB 连接问题 |
| 进入容器跑命令 | `docker compose -f docker-compose.dev.yml exec backend sh` | 临时调试（如查 mounted 文件 / 跑 curl） |
| 容器内 curl 后端 | `docker compose -f docker-compose.dev.yml exec backend wget -qO- http://127.0.0.1:8080/health` | 容器内健康检查 |
| 单服务重启 | `docker compose -f docker-compose.dev.yml restart backend` | 仅后端代码改动（前端 HMR 自动） |
| 关闭 dev 栈 | `docker compose -f docker-compose.dev.yml down` | 测试完毕收尾（**保留** named volumes） |
| 关闭 + 清数据 | `docker compose -f docker-compose.dev.yml down -v` | 脏数据污染后重来（**需 user 授权**，破坏 trial 数据） |

### 11.4 禁止 Agent 直接执行的 Docker 操作

| 操作 | 原因 |
|------|---|
| `docker compose -f docker-compose.yml up -d`（prod compose） | 端口冲突（80/443）+ 容器名前缀 `dc_*` 撞 prod；只用于 ECS 部署 |
| `docker system prune -a` / `docker volume prune` | 销毁性，可能清掉用户其他项目的容器 |
| `docker rm -f $(docker ps -aq)` | 无差别杀容器 |
| 修改 `backend/.env` | §8 红线，含真实 key |
| `docker compose down -v` 不告知 | trial 数据可能丢 |

### 11.5 业务验证 checklist（最小集）

启动 dev 栈后，按顺序验证：

```bash
# 1. 启动 + 看 healthy
docker compose -f docker-compose.dev.yml up -d --build
docker compose -f docker-compose.dev.yml ps
# 期望: dc_dev_postgres / dc_dev_redis / dc_dev_backend / dc_dev_frontend 都 healthy / running

# 2. 业务端点健康检查
curl http://localhost:8180/health
# 期望: {"status":"ok",...}

# 3. v2.1 F4: LLM health
curl http://localhost:8180/api/v1/health/llm | jq
# 期望有 key: {"configured":true,"provider":"deepseek","model":"deepseek-v4-flash","key_preview":"sk-***xxxx"}
# 期望无 key: {"configured":false,...}

# 4. v2.1 F5: metrics 翻动（需先建 1 个 trial）
curl http://localhost:8180/api/v1/metrics | jq '.counters, .gauges'
# 期望: agent_gateway_llm_total_tokens_per_call 在 500-2000

# 5. 前端验证（用 web-gui-tester 或手动）
# - 首页 http://localhost:3000 加载正常
# - 无 key 时: 顶部 amber banner 显示（不可关闭）
# - 建 trial → quick mode 全流程跑通

# 6. 测试完毕收尾
docker compose -f docker-compose.dev.yml down
```

### 11.6 关键纪律

1. **必须先告知再启动**：启动 dev 栈会占 5432/6379/8180/3000 端口，先看 user 是否有其他进程占用
2. **超时保护**：dev 栈首次 `--build` 可能 5-10 分钟（Next.js + Go 依赖下载），用 `command_timeout: 600000`（10 分钟）
3. **不污染 prod**：`dc_dev_*` 容器名前缀 + 8180 端口 + 命名 volumes `dc_dev_*`，与 prod 栈物理隔离
4. **测试完必须 down**：跑完不关 → 5432/6379 端口长期占着 → 影响 user 其他工作
5. **异常上报**：容器反复 restart / healthcheck 不过 / backend fail-fast exit 1 → **不**自动 `down -v` 重来，先看 logs 找 root cause 报 user
6. **§8 红线优先**：任何会写到 `backend/.env` 的操作（即使只是"加一行注释"）一律不做，**用 `$env:VAR='value'` 临时环境变量注入**

### 11.7 异常处理

| 现象 | 排查方向 |
|---|---|
| `dc_dev_backend` 反复 restart | `logs --tail=50 backend` 看 fail-fast 原因（v0.10.18 fail-fast 检查清单在 `/workspace/DecisionCourt/docs/V1-ROADMAP.md` §F1-F7） |
| `dc_dev_postgres` unhealth | 检查 host 5432 端口是否被占 / `docker volume ls` 看 `dc_dev_postgres_data` 是否损坏 |
| Next.js HMR 不生效 | 看 frontend 容器内 `CHOKIDAR_USEPOLLING=true` 环境变量是否生效（v0.9.2 加的跨平台修复） |
| backend 起不来：`DATABASE_URL invalid port` | `.env` 里 `POSTGRES_PASSWORD` 含 `/` 没 URL-encoded（v1.0.3 PR-B1 修过；如复发说明 .env 被手工改坏） |
| `Bind for 0.0.0.0:3000 failed: port already allocated` | host 上有别的进程占 3000；改 `frontend.ports` 或停占端口进程 |
| curl 响应中文显示 `����` / `������ƽ��` | **不是 backend bug**：gin v1.12.0 的 `c.JSON` 默认带 `charset=utf-8`（见 `backend/internal/api/charset_repro_test.go`）。根因是 Windows Git Bash 用 OEM codepage（cp936/cp1252）解码 UTF-8。修法：`curl ... \| jq .`（jq 自动 UTF-8 解码，推荐）/ `chcp 65001` 切 UTF-8 codepage / `curl ... \| iconv -f utf-8 -t utf-8` 强制转码 |

### 11.8 与既有规范的关系

- **§3 测试维护规范**：§3 是"改完跑 `*_test.go`"，**§11 是"跑完测试还要跑 docker 业务"**——两者**互补**，不替代
- **§9 ECS 运维连接**：§9 是"线上 ECS SSH 诊断"，**§11 是"本机 dev compose 业务验证"**——本地 vs 远端
- **§8 敏感文件红线**：§11.6 #6 强调，docker 操作不绕过 §8