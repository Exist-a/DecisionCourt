# DecisionCourt V1 路线图 — 个人项目长期维护

> **版本**：v1.0.0 (2026-08-20 落地) + v1.0.x 系列补丁 + v2.0 视觉升级 + v3.0 端侧 TTS
> **状态**：v1.0.0 已合并（commit `3f1be3b` / `1900813` / `f34051a`），v1.0.x 系列持续推进
> **关联**：[decisioncourt-roadmap.md](./decisioncourt-roadmap.md)（MVP 阶段路线图）+ [release-notes/v1.0.0.md](./release-notes/v1.0.0.md)（v1 发版说明）
> **2026-08-21 重要调整**：
> 1. **去掉商业化路径**（原 v1.1 M2 + M5）— 个人项目长期维护，不规划商业化前置
> 2. **v1.0.5 原方案升级为 v2.0** — 剪影小人 SVG（走路/点头/举手）原属"动画升级最后"，但用户重新评估认为工作量+调试周期都大，**升格为大版本 v2.0**
> 3. **新增 v3.0 端侧 TTS** — 离线 TTS（Piper/Coqui 等），让庭审从"文字"升级为"语音"
> 4. **v1.2 安全 P1 触发后再启动**（不变，但实际优先级低）
> **目标**：从"产品级稳定(v1.0.0)"持续打磨到"v2.0 视觉升级" + "v3.0 多模态语音"，按用户实际反馈动态调整里程碑

---

## 0. 当前进度快照（2026-08-21）

| 维度 | 状态 | 说明 |
|---|---|---|
| v0.10.17-25 系列 | ✅ | silent-error 12 黑洞全修,4 层限流,300 字截断,新意度 / stance 检查 |
| v1.0.0 落地 | ✅ | ECS 30 天 8 问题全修 + DeepSeek v4 迁移 + 8 问题专项回归测试护栏 |
| v1.0.1 | ✅ | v1.0.0 遗留 4 类预存在失败 100% 修复 + prompt HA-001 |
| v1.0.2 | ✅ | 候选 4 已反驳证据集合跟踪 + ADR 0030 |
| **v1.0.3 PR-B1** | ✅ | Prompt Lab baseRules YAML 化 + 热加载 + 5 sub-test (`f1720f7`) |
| **v1.0.3 PR-B2** | ⏸ | Eval + A/B + REST API（待启动） |
| **v1.0.3 PR-B3** | ⏸ | release notes + tag（PR-B2 后） |
| **ArgumentMap 移除** | ✅ | ADR 0032 + `16332aa`，用户反馈"完全没用" |
| **dozzle 移除** | ✅ | `29daff0`，Windows Docker Desktop npipe 不兼容 |
| **D2 + D3 修复** | ✅ | `694a89e`，cross-exam silent error + 直接判决 fallback "共 0 轮" |
| ADR 累计 | 32 | 含 0031 Prompt Lab + 0032 Remove ArgumentMap |
| Go 测试 | ~290 sub-test | v1.0.0 263 + PR-B1 11 + D2/D3 9 + 其他修补 |
| Frontend 测试 | 7 .test.ts | v1.0.3 移除 ArgumentMap 0 测试 |
| 部署目标 | ⏸ 本地 dev | ECS 2026-08-05 终止,转入个人长期本地开发模式 |

**当前阻塞**：无。下一决策是用户授权 PR-B2（v1.0.3 补完）。

---

## 1. 总体策略

### 1.1 阶段切分原则

- **v1.0.x 是"补完 + 巩固"**:把 v0.10.x 留下的功能候选项和 ECS 沉淀修掉,不动架构
- **v1.1+ 是"前置 + 商业化"**:用户系统 / 多租户 / 订阅,任何公网部署前必须有
- **v2.0 是"产品 2.0 飞跃"**:多模态(语音庭审) / 实时协作 / 行业垂直(法律/医疗)

### 1.2 优先级原则

- **用户触发 > 自动推进**:每个里程碑必须有用户授权才能开(避免过度工程)
- **回归测试 > 新功能**:v1.0.0 起的 PR-3 回归测试框架是后续所有 PR 的强制 baseline
- **文档先行**:每个里程碑启动前先 ADR 入库 + release notes 草稿

### 1.3 不做什么

- ❌ **不引入新架构**(v1 全程沿用 v0.10 + v1.0.0 已实装的架构)
- ❌ **不重写核心组件**(ReAct runner / Agent Gateway / 信念引擎 v0.6 不动)
- ❌ **不做大规模重构**(保持向后兼容,.env / 数据库 schema 兼容)
- ❌ **不接 OTLP / Jaeger**(等 Phase D 触发条件:多实例部署)
- ❌ **不接 ClickHouse**(等 Phase E 触发条件:商业化启动)

---

## 2. 里程碑(M0 → M4)

### M0:v1.0.0 落地 ✅ (2026-08-20)

**目标**: ECS 30 天沉淀收尾 + DeepSeek v4 迁移 + 8 问题专项回归测试护栏

**已完成**:
- ✅ PR-1 P3-1 IP 脱敏(`3f1be3b`)
- ✅ PR-2 DeepSeek v3→v4 硬迁移(`1900813`)+ ADR 0029
- ✅ PR-3 ECS 8 问题专项回归测试(`f34051a`)
- ✅ PR-4 v1.0.0 release notes(本文件 [release-notes/v1.0.0.md](./release-notes/v1.0.0.md))
- ✅ PR-5 v1.0.0 tag + push(commit `6b35e92`)

### M0.5:v1.0.1 收尾 ✅ (2026-08-20)

**目标**: 修掉 v1.0.0 release notes ⏸ 已知遗留的 4 类预存在测试失败(用户授权:"在 1.0 开始之前得把错误都修了")

**已完成**:
- ✅ baseRules 第 16 条 (HA-001 调查发现 vs 用户证据混淆)
- ✅ buildInvestigationContext + ProsecutorPrompt / DefenderPrompt 接入
- ✅ FileLogger.Close() 改进(置 nil + lazy reopen)避免 Windows file lock
- ✅ 8 个 ReActRunner 测试 stale 断言同步到 v0.10.23/24 实际 call 数
- ✅ TranscriptContextInjected onCall 改为 per-call 切片数组
- ✅ v1.0.1 release notes ([release-notes/v1.0.1.md](./release-notes/v1.0.1.md))
- ✅ `go test ./...` 100% PASS

**Commit**: `ae7a464`(5 files, +91/-20)

**下次行动**: 打 v1.0.1 tag + push(待用户授权)

---

### M1:v1.0.2 候选 4 已反驳证据集合跟踪 ✅ (2026-08-20)

**目标**: 实现 PRD §4.3.3 "v0.7+ 计划" 第一项 "禁止引用被反驳且未翻盘的证据"。
roadmap §0 顶部钦定 "下一步进入候选 4 讨论"。

**已完成** (6 PR, 5 commits):
- ✅ PR-1 数据模型 + ADR 0030 (`ac8eda0`)
- ✅ PR-2 输入协议 (AgentOutput.Rebut + RebuttalHook + RebuttalSink) (`a2eddb6`)
- ✅ PR-3 后端硬拒 (applySpeakerRebuttalCheck, 与 stance/novelty 同级 guard) (`45dce74`)
- ✅ PR-4 Service 集成 + GORM RebuttalRepository + REST /rebuttal-links (`0d43e76`)
- ✅ PR-5 baseRules rebut schema + Frontend EvidenceBoard ⚔ 已反驳 X 次 chip (`88e5178`)
- ✅ PR-6 ecs_regression_v102_test + v1.0.2 release notes + tag
- ✅ `go test ./...` 100% PASS (含新增 13 sub-test)
- ✅ `pnpm test` 40/40 PASS
- ✅ `pnpm tsc --noEmit` 通过

**Commit 链**: `ac8eda0` → `a2eddb6` → `45dce74` → `0d43e76` → `88e5178` → (PR-6)

**触及 §2.1 裁决逻辑**: **中** (PRD §4.3.3 hard reject, 与 stance/novelty 同等级; 法官判决不感知 → 留 v1.0.x 后续讨论)

**回滚**: `git reset --hard v1.0.1` 回 v1.0.1 + 单独 revert 任一 PR

详见 [release-notes/v1.0.2.md](./release-notes/v1.0.2.md) + [ADR 0030](./adr/0030-evidence-rebuttal-state-machine.md)

---

### M2:v1.1 商业化前置 (⏸ 用户系统需求触发)

**目标**: 公网部署前的最小商业化能力

**子项**:

| 子项 | 工作量 | 触发 |
|---|---|---|
| 用户系统(注册 / 登录 / 个人中心) | 1 week | 用户主动要求 |
| 多租户隔离(企业版预留) | 2 weeks | 第一个企业客户 |
| 订阅 + 计费(Stripe / 国内支付) | 1 week | 商业化启动 |
| 限额分层(免费 / Pro / 企业) | 3 days | 商业化启动 |
| 用户级私有记忆(替代 session 级) | 1 week | 用户跨设备同步需求 |

**关键约束**:
- **必须跑过本地 dev compose 完整链路**(v1.0.0 已具备能力)
- **必须接 CI**(GitHub Actions CI 暂停机制需要重启,v1.0.0 不阻塞)
- **必须支持 oss 用户系统**(邮箱 + 密码 + 第三方 OAuth)

**触及 §2.1 裁决逻辑**: **0**(纯基础设施)

---

### M3:v1.2 安全 P1 阶段(⏸ 企业客户触发 deferred D1)

**目标**: D1 安全审计 14 项 P1 全部落地,为公网部署铺路

**来源**: [`docs/todo/deferred-items-2026-08-05.md` §D1](./todo/deferred-items-2026-08-05.md)

**子项**:

| 编号 | 项 | 复杂度 |
|---|---|---|
| P1-1 | WS Origin CheckOrigin 白名单(env 已支持,需补 Caddy 配置示例) | 1 day |
| P1-2 | CSRF Token 中间件(HttpOnly cookie + double-submit) | 2 days |
| P1-3 | 输入长度校验(query / evidence / verdict 长度上限) | 1 day |
| P1-4 | LLM prompt 注入防护(context 隔离 + sanitize) | 2 days |
| P1-5 | 日志脱敏(Detail 字段 prod 不填充 Go 内部信息)— v1.0.0 P3-1 部分完成 | 0.5 day |
| P1-6 | 依赖固定版本 + `npm audit` / `govulncheck` CI | 1 day |
| P1-7 | dev mode → prod 模式硬开关(GIN_MODE + frontend NODE_ENV) | 0.5 day |

**总工作量**:~8 days / 1 人

**触及 §2.1 裁决逻辑**: **0**(纯防御性配置)

**触发**: 企业客户 / 公网部署 / 安全事件

---

### M4:v2.0 产品 2.0 飞跃 (⏸ 商业化稳定 6 个月后)

**目标**: 从"辩论系统"升级到"决策操作系统"

**子项**:

| 子项 | 描述 | 触发 |
|---|---|---|
| **多模态庭审** | 语音输入 + TTS 输出 + 庭审录音回放 | 用户明确要求 |
| **实时协作** | 多人同时旁听 + 评论 + 投票 | 企业版需求 |
| **行业垂直** | 法律(合同审查) / 医疗(诊断决策) / 投资(M&A) | 行业客户 |
| **决策执行** | 判决书输出后,自动创建任务清单 + 跟进 + 复盘 | 用户主动要求 |
| **AI 法官介入** | 当双方僵持,AI 法官主动提问打破 | 复杂决策场景 |
| **OSS 化运营** | 公开判决书库 + 社区投票 + 案例检索 | 商业化稳定后 |

**触及 §2.1 裁决逻辑**: **强**(AI 法官介入 / 判决书执行)

**前置**: M1 + M2 + M3 全部落地 + 用户量 > 100 + 至少 1 个企业客户

---

## 3. 技术依赖关系（2026-08-21 重规划：v2.0 = 剪影小人，v3.0 = 端侧 TTS）

```
M0 v1.0.0 ✅ ─┐
M0.5 v1.0.1 ✅│
M1 v1.0.2 ✅ ─┴─ M2 v1.0.3 ( PR-B1 ✅ / PR-B2 + B3 ⏸ ) ── M3 v1.0.4 ⏸ ── M4 v2.0 ⏸ (剪影小人)
                                                                                  │
                                                                                  └─ M5 v3.0 ⏸ (端侧 TTS)
M6 v1.2 ⏸ (安全 P1, 触发后启动)
```

- **v1.0.3 PR-B2/B3** → v1.0.4 顺序依赖（PR-B2 完成后启动 v1.0.4）
- **v1.0.4** → v2.0 顺序依赖（Trace + Framer Motion 给 v2.0 提供动画基础设施）
- **v2.0** → v3.0 松散依赖（v2.0 提供"角色"概念，v3.0 给角色加声音；可独立推进）
- **v1.0.x / v2.0 / v3.0** 与 v1.2 / v2.0 远期 无强依赖（三线独立：产品打磨 / 安全 / 远期）

---

## 4. 关键里程碑（2026-08-21 重规划）

| 里程碑 | 计划时间 | 标志 | 实际状态 |
|---|---|---|---|
| M0 v1.0.0 | 2026-08-20 | ECS 8 问题全修 + DeepSeek v4 + 263 sub-test | ✅ |
| M0.5 v1.0.1 | 2026-08-20 | v1.0.0 遗留测试 100% 修复 + prompt HA-001 | ✅ (`ae7a464`) |
| M1 v1.0.2 | 2026-08-20 | 候选 4 已反驳证据集合跟踪 + ADR 0030 | ✅ (`ac8eda0`+...) |
| **M2 v1.0.3 PR-B1** | 2026-08-21 | Prompt Lab baseRules YAML 化 + 热加载 + 5 sub-test | ✅ (`f1720f7`) |
| **M2 v1.0.3 PR-B2** | v1.0.3 后 | Eval + A/B + REST API | ⏸ ([V1.0.3-PLAN.md](./V1.0.3-PLAN.md) §3 PR-B2) |
| **M2 v1.0.3 PR-B3** | PR-B2 后 | release notes + tag | ⏸ ([V1.0.3-PLAN.md](./V1.0.3-PLAN.md) §3 PR-B3) |
| **M3 v1.0.4** | v1.0.3 后 | LLM Trace 可视化 + Framer Motion 微动效 | ⏸ ([V1.0.4-PLAN.md](./V1.0.4-PLAN.md)) |
| **M4 v2.0 视觉升级** | v1.0.4 后 | 剪影小人 SVG + 走路/点头/举手 | ⏸ ([V2.0-PLAN.md](./V2.0-PLAN.md)，原 V1.0.5-PLAN.md 升级） |
| **M5 v3.0 端侧 TTS** | v2.0 后 | 离线 TTS（Piper/Coqui 等），庭审语音化 | ⏸ ([V3.0-PLAN.md](./V3.0-PLAN.md)) |
| M6 v1.2 | 公网部署 / 安全事件 / 个人决定 | D1 安全 P1 全部落地 | ⏸ |

**已移除**：
- ~~M5 v1.1 商业化前置~~（2026-08-21 决定：个人项目长期维护，不规划商业化）
- ~~V1.0.5 / V1.0.6 占位~~（重新规划：剪影小人升级为 v2.0，端侧 TTS 新增为 v3.0）

---

## 5. 风险与应对（2026-08-21 重规划）

| 风险 | 影响 | 应对 |
|---|---|---|
| 候选 4 触动裁决逻辑（强 §2.1） | 影响最终判决计算 | 先 ADR 详细讨论 + 用户明确授权 |
| v1.0.3 PR-B2/B3 拖延 | v1.0.3 永远"半发布" | 每 PR 用户可暂停,但完成后才 tag |
| v1.0.4 Trace 后端性能问题（1 trial ~50 runs） | API timeout | LRU 缓存 + 日期分区 + 滚动归档 |
| D1 安全审计 14 项遗漏（个人项目，未来公网部署时） | 公网部署被攻击 | v1.2 触发后再启动 |
| v2.0 多模态投入过大 | 项目方向漂移 | 远期参考，不绑定触发条件 |
| ArgumentMap 类 UI 组件"用户根本不会看" | 浪费开发资源 | 用户反馈驱动（ADR 0032 教训） |
| Dev compose 启动配置脆弱（DATABASE_URL 拼接 / NEXT_PUBLIC_* 覆盖 / Windows npipe） | Windows / Linux 切换困难 | 文档化 + 简化为hardcode；回归测试（agent_dev compose E2E）|
| Silent error 黑洞（react_runner 流式解析 silent fail） | 庭审记录全空但不报错 | D2 fix (`694a89e`) 加 WARN 日志 + 拦截；D3 fix 同 commit |

---

## 6. 下一步（2026-08-21 重规划 v2/v3）

### 立即可做（等用户授权）

1. **v1.0.3 PR-B2 启动**（Eval + A/B + REST API）— [V1.0.3-PLAN.md §3 PR-B2](./V1.0.3-PLAN.md)
2. **v1.0.3 PR-B3 启动**（release notes + tag）— PR-B2 完成后
3. **v1.0.4 启动**（LLM Trace 可视化 + Framer Motion 微动效）— v1.0.3 PR-B3 完成后，按 [V1.0.4-PLAN.md](./V1.0.4-PLAN.md) 推进
4. **v2.0 启动**（剪影小人 SVG + 走路/点头/举手）— v1.0.4 完成后，按 [V2.0-PLAN.md](./V2.0-PLAN.md) 推进（原 V1.0.5-PLAN.md 升级为大版本）
5. **v3.0 启动**（端侧 TTS）— v2.0 完成后，按 [V3.0-PLAN.md](./V3.0-PLAN.md) 推进

### 长期（按需触发）

6. **v1.2 安全 P1** — 触发条件：公网部署 / 安全事件 / 用户决定继续安全加固

### 持续维护

- **静默错误黑洞回归测试护栏**（v1.0.0 PR-3 框架 + D2/D3 9 sub-test 在 `694a89e`）
- **DeepSeek API 文档变更跟进**（ADR 0029 教训）
- **Dev compose 回归测试**（host 端口冲突 + env 优先级 + Windows npipe）
- **AGENTS.md §8 敏感文件红线**（项目长期规范）

---

## 关联文档

- [decisioncourt-roadmap.md](./decisioncourt-roadmap.md) - MVP 阶段路线图(M0 之前)
- [release-notes/v1.0.0.md](./release-notes/v1.0.0.md) - v1.0.0 发版说明
- [todo/deferred-items-2026-08-05.md](./todo/deferred-items-2026-08-05.md) - 安全审计 P1-P3 deferred
- [roadmap/whitebox-roadmap.md](./roadmap/whitebox-roadmap.md) - 可观测性 Phase A-E
- [project-ideas.md](./project-ideas.md) - 未来灵感池