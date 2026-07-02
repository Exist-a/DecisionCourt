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

### 6.2 进行中的设计文档（`.trae/documents/` 目录）
- `memory-a2a-redesign.md` - **v0.5 记忆系统 + A2A 重设计**（Episodic Memory via A2A、ContextView 投影、前端 MemoryAuditPanel、SessionUUID 房间钥匙 bug 修复、MemoryEntry 结构化字段）。当前版本 v1.1（2026-06-30），所有 PR 已完成。
- `todolist1-pr1-contextview.md` - PR 1 ContextView 投影详细规划
- `庭审可视化简化计划.md` - 庭审页面视觉简化
- `质证阶段轮次控制修改计划.md` - cross-exam 阶段轮次控制

> **注意**：进行中的设计文档优先级不亚于主文档。修改相关代码前必须先读对应的 `.trae/documents/` 计划 —— 这些是已经过用户确认的执行方案，不是草稿。

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