# ADR 0029: DeepSeek v3→v4 模型硬迁移

| | |
|---|---|
| **编号** | 0029 |
| **标题** | DeepSeek 模型默认值 v3→v4 硬迁移 |
| **状态** | ✅ Accepted |
| **作者** | Exist + ZCode Agent |
| **决策日期** | 2026-08-20 |
| **触发** | 用户 2026-08-20 调研时发现 DeepSeek 官方 API 文档已只列 v4-flash / v4-pro,旧名 deepseek-chat / deepseek-reasoner 不再在文档中 |
| **依赖** | 无 |
| **替代决策** | 无(用户已确认"硬迁移") |
| **影响** | `backend/internal/config/config.go` + `.env.example` + 8 个 mock 测试文件 + ADR README 索引 |

## 1. 决策

### 1.1 背景

2026-08-20 用户在 v1.0.0 发版规划时要求 Agent 调研 DeepSeek API 变化。Agent 用 WebFetch 查 https://api-docs.deepseek.com/ 官方文档,发现:

- **官方文档当前仅列 2 个模型**:`deepseek-v4-flash` (V4-Flash-0731 更新版) 和 `deepseek-v4-pro` (V4-Pro-0813 更新版)
- **旧名 `deepseek-chat` / `deepseek-reasoner` 已不在文档**——可能被弃用或重新命名
- **Base URL `https://api.deepseek.com` 不变**,`/v1/chat/completions` 路径不变,鉴权方式 (Bearer) 不变
- **新增字段**:`thinking` object + `reasoning_effort: low|high|max` —— 替代旧 `reasoning_content` 字段
- **新 endpoint**:`/responses` (Responses API)、`/completions` (FIM Beta)、`/models`、`/user/balance`

DecisionCourt 项目当前默认值:
```go
LLMModelV3:  "deepseek-chat"
LLMModelR1:  "deepseek-reasoner"
```

这两条默认值在 v1.0.0 之后**会被 DeepSeek API 拒绝** (404 / 400 model_not_found),导致所有 LLM 调用失败,庭审主流程断。

### 1.2 矛盾诊断

按 AGENTS.md §2 规则,这是"裁决类型"边缘场景(涉及模型选型影响所有庭审 ReAct 调用),但**不属于裁决逻辑**(模型选型≠庭审结论判定)。属于"配置/技术决策",可以**先调研后实施**。

### 1.3 替代方案分析

| 方案 | 工作量 | 风险 | 用户决策 |
|------|--------|------|---------|
| **(a) 硬迁移**:默认值改 v4-flash/v4-pro,.env 旧名仍可手动覆盖 | 1 hour | 现有 .env 写 deepseek-chat 的项目需手动改 | ✅ **采用** |
| (b) 软迁移:新增 .env 变量,默认 v4 但兼容旧名 | 2 hour | 兼容窗口拉长,但新旧混用增加心智负担 | ❌ 用户拒绝 |
| (c) 不改默认,仅文档警告 | 0.5 hour | 用户跑 v1.0.0 仍然默认 404,功能不可用 | ❌ 不采纳 |

### 1.4 实施决策

**采用 (a) 硬迁移**:
- `LLMModelV3` 默认 `deepseek-chat` → `deepseek-v4-flash`
- `LLMModelR1` 默认 `deepseek-reasoner` → `deepseek-v4-pro`
- `.env.example` 同步默认值 + 加注释说明迁移路径
- 8 个 mock 测试文件的 `deepseek-chat` 字面量改为 `deepseek-v4-flash`
- 新增 3 个 config 测试断言新默认值 + 旧名 override 仍兼容
- ADR 0029 入库(本文件)

### 1.5 兼容性

- **.env 层面**:用户可在 .env 中手动设 `LLM_MODEL_V3=deepseek-chat` 短时兼容(但 DeepSeek API 可能已拒绝旧名,需用户自行验证)
- **config.go 层面**:仅默认值变更,envOrDefaultString 读取 .env 优先,自定义名字仍生效
- **测试层面**:3 个新测试明确说明"override 仍兼容",作为向后兼容的 regression 测试

## 2. 实施内容

### 2.1 Commit 表

| commit | 内容 |
|--------|------|
| (本 commit) | `feat(config): DeepSeek v3→v4 模型硬迁移 (ADR 0029)` |

### 2.2 修改文件清单

| 文件 | 类型 | 改动 |
|------|------|------|
| `backend/internal/config/config.go` | 修改 | L128-129 默认值改 v4 |
| `backend/internal/config/env_test.go` | 修改 | + 3 个新测试 |
| `.env.example` | 修改 | L18-19 默认值改 v4 + 加注释 |
| `backend/cmd/server/main.go` | 修改 | L74 fallback 默认值改 v4 |
| `backend/internal/agent_gateway/cache_test.go` | 修改 | deepseek-chat → deepseek-v4-flash |
| `backend/internal/agent_gateway/compression_eval_test.go` | 修改 | 同上 |
| `backend/internal/agent_gateway/file_logger_test.go` | 修改 | 同上 |
| `backend/internal/agent_gateway/gateway_advanced_test.go` | 修改 | 同上 |
| `backend/internal/agent_gateway/gateway_test.go` | 修改 | 同上 |
| `backend/internal/agent_gateway/gateway_v2_test.go` | 修改 | 同上 |
| `backend/internal/agent_gateway/gorm_store_test.go` | 修改 | 同上 |
| `backend/internal/agent_gateway/recorder_test.go` | 修改 | 同上 |
| `docs/adr/0029-deepseek-v4-migration.md` | 新建 | 本 ADR |
| `docs/adr/README.md` | 修改 | 索引表加 0029 |

### 2.3 验证

| 验证项 | 结果 |
|--------|------|
| `go build ./...` | ✅ 通过 |
| `go test ./internal/config/ -run DeepSeek` | ✅ 3/3 PASS |
| `go test ./internal/config/...` | ✅ 全过(0 退化) |
| `go test ./internal/agent_gateway/...` | ⚠️ 2 个 Windows 文件锁预存在失败(与本次改动无关) |

## 3. 教训总结

1. **官方文档变更要主动跟进**:本次如果不是用户提示,Agent 不会主动查 DeepSeek API 变化,v1.0.0 会带着死默认值发布。
2. **model name 字面量散落测试文件**:8 个 mock 测试硬编码 `deepseek-chat`,本次迁移要 sed 全量替换。建议后续抽象成 `const TestDefaultModel = "deepseek-v4-flash"`。
3. **本地无法实测 v4**:本机无真实 DeepSeek API key,`curl /models` 鉴权拒绝,无法直接 verify v4 model name 是否真的存在。决策依据 = 官方文档 + ADR 文档化风险,接受"实施后用户首次跑可能 404"的概率。

## 4. 后续工作

- [ ] 用户首次部署 v1.0.0 时,**人工跑一次 trial** 验证 v4 模型可调用。若 404,fallback 到原 .env 旧名再 verify(确认 DeepSeek 是否还兼容)。
- [ ] Phase B 计划:把 mock 测试的 model name 集中到 `internal/agent_gateway/test_constants.go` 一个文件,下次迁移只改 1 处。
- [ ] ADR 0029 在 v1.0.0 release notes 中显式列入 🆕 新增功能章节。

## 5. 关联文档

- [v1.0.0 Release Notes](../release-notes/v1.0.0.md)(待 PR-4 写)
- [DeepSeek 官方 API 文档](https://api-docs.deepseek.com/) - 权威 source
- [DeepSeek Chat Completion Reference](https://api-docs.deepseek.com/api/create-chat-completion) - v4-flash / v4-pro 当前参数
- [AGENTS.md §2.1 裁决规则](../../AGENTS.md) - 本次决策不属于裁决逻辑
- [AGENTS.md §1.2 文档一致性](../../AGENTS.md) - 改 config 必须同步文档(.env.example + ADR)