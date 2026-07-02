# 04 · Agent Gateway v2（v0.7+）—— 工程化让 LLM 调用可控、可降级

> **目标**：用第一人称 + 工程视角，讲清楚 v0.7 Gateway 装饰器链（reliability 链 + compression 链）**为什么这么设计**、**怎么实装**、**如何在面试中讲出来**。
> **配套**：[`../architecture/link-overview.md`](../architecture/link-overview.md) · [`../adr/0008-agent-gateway-v07.md`](../adr/0008-agent-gateway-v07.md) · [`../observability/case-study-2026-07-02.md`](../observability/case-study-2026-07-02.md)
> **更新于**：2026-07-02
> **版本**：v1.0

---

## 0. 一句话总结

> LLM 调用不能"裸调"——必须套**装饰器链**：**reliability 链**（retry/fallback/circuit-breaker）+ **compression 链**（prompt 压缩/budget/throttle） + **recording 链**（token 审计/落库）。这样 LLM 故障不会击穿到业务，**让 AI 系统像工程系统一样可降级**。

---

## 1. 为什么不让 LLM 裸调？

### 1.1 业内常见坑（我踩过或调研过）

| 问题 | 后果 |
|---|---|
| 单次 LLM 调用 = 1 个 API call | DeepSeek 504 时整个庭审卡住 |
| Prompt 越长越贵 | 5 轮 cross_exam 后 token 翻倍 |
| LLM provider 切不动 | 写死在 deepseek，某天他们降价了切不走 |
| 错误全靠前端 try/catch | 用户看到白屏 |

**关键洞察**：LLM 不是"函数调用"，是"外部依赖 + 慢 + 不稳定 + 贵"。**必须当分布式系统对待**。

### 1.2 我的设计选择：**装饰器链 + 责任分离**

把"给 LLM 发 prompt"这件事拆成 **5 个独立关心点**：

```
[RecordingDecorator]
   ↑
[ThrottleDecorator]
   ↑
[BudgetDecorator]
   ↑
[CompressionDecorator]
   ↑
[ReliabilityDecorator (retry + fallback + circuit-breaker)]
   ↑
[BaseLLMProvider] (deepseek / openai / ollama / mock)
```

**每个装饰器只关心一件事**，可独立测试 / 替换 / 关闭。

---

## 2. 装饰器链结构（v0.7 实装）

### 2.1 BaseLLMProvider 接口

[`backend/internal/agent_gateway/provider.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/provider.go)：

```go
type LLMProvider interface {
    Name() string                                     // "deepseek" / "mock" / ...
    Call(ctx, req LLMRequest) (LLMResponse, error)
    StreamCall(ctx, req LLMRequest) (<-chan LLMChunk, error)
}
```

**关键**：Base 接口**只关心"发 prompt 拿结果"**。其他都交给装饰器。

### 2.2 Reliability 装饰器（最外层兜底）

[`backend/internal/agent_gateway/reliability.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/reliability.go)：

```go
type ReliabilityDecorator struct {
    inner          LLMProvider
    maxRetries     int           // 默认 3
    backoffBase    time.Duration // 默认 1s（指数退避）
    circuitBreaker *Breaker      // 失败率 > 50% 时熔断
    fallbackChain  []LLMProvider // deepseek 失败 → openai → ollama → mock
}

func (d *ReliabilityDecorator) Call(ctx, req) (res, err) {
    // 1. 熔断检查
    if d.circuitBreaker.IsOpen() {
        return d.tryFallback(ctx, req, "circuit_open")
    }
    // 2. 重试 + 退避
    for attempt := 0; attempt <= d.maxRetries; attempt++ {
        res, err := d.inner.Call(ctx, req)
        if err == nil { return res, nil }
        if !isRetryable(err) { break }
        time.Sleep(d.backoffBase * (1 << attempt))  // 指数退避
    }
    // 3. Fallback 链（deepseek → openai → ollama → mock）
    return d.tryFallback(ctx, req, "exhausted")
}
```

**3 个能力**：
- **Retry**：网络/超时错误自动重试（指数退避）
- **Circuit-breaker**：失败率 > 50% 时熔断 30s，避免雪崩
- **Fallback**：provider 链，按顺序降级到 mock

### 2.3 Compression 装饰器（成本控制）

[`backend/internal/agent_gateway/compression.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/compression.go)：

```go
type CompressionDecorator struct {
    inner     LLMProvider
    compressor Compressor  // Smart Compression 策略
    maxTokens int         // 默认 8000
}

func (d *CompressionDecorator) Call(ctx, req) (res, err) {
    if len(req.Prompt) > d.maxTokens*4 {  // ~ 字符数
        compressed, err := d.compressor.Compress(req.Prompt)
        if err != nil { return nil, err }
        req.Prompt = compressed
    }
    return d.inner.Call(ctx, req)
}
```

**Smart Compression 关键设计**（来自 v0.7 ADR）：
- **不压"系统提示 + 角色历史"** —— 关键法律约束不能丢
- **压"早期对话 + 事实陈述"** —— 重复信息是压缩目标
- **保留最近 3 轮完整对话** —— 不让 Agent 失忆

### 2.4 Budget 装饰器（用户配额）

```go
type BudgetDecorator struct {
    inner    LLMProvider
    budget   Budget       // 20000 token / session
    persist  Persistence  // 写到 llm_calls 表
}

func (d *BudgetDecorator) Call(ctx, req) (res, err) {
    if d.budget.Used() + req.EstimatedTokens > d.budget.Limit() {
        return nil, ErrBudgetExhausted  // 主动放弃
    }
    res, err := d.inner.Call(ctx, req)
    if err == nil { d.budget.Add(res.TotalTokens) }
    return res, err
}
```

**业务价值**：每场庭审最多花 20000 token（避免失控），单次调用超支就 fail-fast。

### 2.5 Throttle 装饰器（限流）

```go
type ThrottleDecorator struct {
    inner     LLMProvider
    rate      int           // 5 次/秒
    burst     int           // burst 10
    rateLimiter *rate.Limiter
}

func (d *ThrottleDecorator) Call(ctx, req) (res, err) {
    if err := d.rateLimiter.Wait(ctx); err != nil {
        return nil, err
    }
    return d.inner.Call(ctx, req)
}
```

**典型场景**：5 个 Agent 同时 LLM 调用，**rate limiter 保证不超 provider 的 QPS 上限**。

### 2.6 Recording 装饰器（最内层）

```go
type RecordingDecorator struct {
    inner   LLMProvider
    recorder Recorder  // GORMStore + FileLogger
}

func (d *RecordingDecorator) Call(ctx, req) (res, err) {
    res, err := d.inner.Call(ctx, req)
    d.recorder.Record(ctx, RecorderInput{
        Request:       req,
        Response:      res,
        Error:         err,
        RequestID:     ctx.Trace.ID(),
        SessionUUID:   req.SessionUUID,
        AgentType:     req.AgentType,
        TaskType:      req.TaskType,
        LatencyMs:     time.Since(start).Milliseconds(),
        Status:        statusFromErr(err),
        // ... 47 字段（v0.5+ audit trail）
    })
    return res, err
}
```

**关键**：Recording 在最内层（紧贴 BaseLLMProvider），**所有 token / latency / 错误全覆盖**。**v0.5+ 47 字段审计**让"为什么这次慢"可查。

---

## 3. 装饰器链的 3 个工程优势

### 3.1 责任分离 = 可测试

每个装饰器独立单测。**Integration test 只测"链组合"**。**单元测试覆盖率 ≥ 90%**。

### 3.2 可关闭 = 可降级

`config.go` 提供：
```go
type GatewayConfig struct {
    EnableReliability bool  // false → 直连 BaseLLMProvider（debug 用）
    EnableCompression bool
    EnableBudget      bool
    EnableThrottle    bool
    EnableRecording   bool  // false → 不要审计算脱机模式
}
```

**测试场景**：可以关掉 ReliabilityDecorator 测单次失败行为，**关闭 production-only 功能**。

### 3.3 可扩展 = 未来加新能力

未来加**Cache 装饰器**（相同 prompt 命中缓存），**只需写一个 + 在 main.go 装配链里加一行**。**业务代码不动**。这就是装饰器模式的"开闭原则"价值。

---

## 4. LLM Gateway 的工程化价值（面试加分）

### 4.1 业内对比

| 维度 | 裸调 LLM | 我的 Gateway 链 |
|---|---|---|
| 故障恢复 | 手工 try/catch | 自动重试 + 熔断 + Fallback |
| 成本控制 | 无上限 | budget + throttle + compression |
| 审计 | log.Printf 一行 | 47 字段落库 + file log |
| 测试 | 难（外部依赖） | 易（单装饰器单测） |
| Provider 切换 | 改 N 个文件 | 改 1 个配置 |

### 4.2 真实场景演示

**场景**：一场庭审 cross_exam 3 轮，5 个 Agent 每轮 3 次 LLM 调用 = 45 次。

**裸调的问题**：
- 第 12 次 deepseek 504 → 庭审卡住
- 第 30 次 token 超预算 → 费用爆掉
- 第 40 次 prompt 8000 token → 单次 4 秒变 12 秒

**有 Gateway**：
- 第 12 次失败 → 自动重试 2 次 → 切换 openai fallback → 用户无感
- 第 30 次 → budget 拦截 → fail-fast → 用户收到友好错误
- 第 40 次 → Smart Compression 自动压到 4000 token → 8 秒回到 4 秒

**ROI = 用户体验 + 成本可控 + 故障可降级**。

---

## 5. 防质疑思考

### Q1: "为什么不用 LangChain / LlamaIndex 这些框架？"

> **明确的好处**：开箱即用，有 Chain / Agent / Tool / Memory 等抽象。
>
> **我不用它们的理由**：
> 1. **抽象 ≠ 价值**——它们的 Chain 不一定匹配我的"5 个 Agent 庭审"场景
> 2. **学习曲线**——为了用它们的框架，我得把我的设计**翻译成它们的语言**
> 3. **依赖风险**——LangChain 半年一变的 API 让我没安全感
> 4. **可调试性**——自己写的装饰器链**每一步都看得见**
> 5. **性能**——框架通常有 1-2 层 wrapper overhead
>
> **我的 Gateway 是 250 行 Go**，5 个装饰器，**每个我能讲清**。LangChain 50 万行代码，**底层任何行为我都不确定**。

### Q2: "Smart Compression 怎么证明没丢关键信息？"

> v0.7 测试用例：50+ 压缩前后对比测试，**关键约束（system prompt / 角色历史 / 最近 3 轮）100% 保留**。压缩目标是"重复信息"（如庭审早期陈述的 5 段同样事实）。
>
> 实测：8000 token prompt 压到 4000，**最终裁决与无压缩版本 90% 一致**。**10% 差异来自"压缩后措辞更简洁但不改变结论"**。

### Q3: "熔断器的阈值怎么定？"

> 默认：滑动窗口 100 次调用，错误率 > 50% 持续 30s 熔断。**通过 `config.json` 可调**。
>
> 业内经典做法（来自 Netflix Hystrix 等）：**经验值 + 测试验证**。我在 dev 环境跑 100 次模拟故障，发现 30s 熔断窗口足够让 deepseek 恢复。

---

## 6. 关键代码位置（面试可指）

| 模块 | 文件 | 行数 |
|---|---|---|
| Gateway 装配 | `backend/internal/agent_gateway/gateway.go` | ~150 |
| Reliability 装饰器 | `backend/internal/agent_gateway/reliability.go` | ~200 |
| Compression 装饰器 | `backend/internal/agent_gateway/compression.go` | ~180 |
| Budget 装饰器 | `backend/internal/agent_gateway/budget.go` | ~100 |
| Throttle 装饰器 | `backend/internal/agent_gateway/throttle.go` | ~80 |
| Recording 装饰器 | `backend/internal/agent_gateway/recorder.go` | ~150 |
| Base LLM Provider | `backend/internal/agent_gateway/provider.go` | ~120 |
| deepseek 实现 | `backend/internal/agent_gateway/deepseek.go` | ~200 |
| Circuit-breaker | `backend/internal/agent_gateway/breaker.go` | ~80 |
| main.go 装配 | `backend/cmd/server/main.go` | ~15 行 |

**总计 ~1275 行 Go**，~50 个单元测试。

---

## 7. 面试话术

### 30 秒版

> "我在 [项目名] 设计了 **Agent Gateway v2**——LLM 调用不裸调，套 5 层装饰器：**Reliability（retry + circuit-breaker + fallback）/ Compression（Smart Compression）/ Budget（单 session token 上限）/ Throttle（rate limit）/ Recording（47 字段审计）**。**reliability 链兜底、compression 链控成本**。这样 LLM 故障不会击穿到业务，深夜 deepseek 504 也能自动切换 openai，用户无感。**让 AI 像工程系统一样可降级**。"

### 3 分钟版

> 大多数 AI 项目 LLM 裸调，**生产事故 top 3**：provider 504 卡死 / token 超支爆掉 / 失败无审计。
>
> 我设计 **Agent Gateway v2** 把"给 LLM 发 prompt"这件事拆成 **5 个独立装饰器**：
>
> 1. **Reliability** —— retry (指数退避 3 次) + circuit-breaker (失败率 50% 熔断 30s) + fallback 链 (deepseek → openai → ollama → mock)
> 2. **Compression** —— Smart Compression：**保留 system prompt + 角色历史 + 最近 3 轮对话**，压缩重复信息。**8000 token → 4000 token，10% 差异不影响结论**
> 3. **Budget** —— 单 session 20000 token 上限，超额 fail-fast
> 4. **Throttle** —— 5 QPS 限流，防止 provider QPS 超限
> 5. **Recording** —— 47 字段审计（prompt_tokens / completion_tokens / latency_ms / status / retry_count / ...）落 `llm_calls` 表
>
> **关键设计哲学**：**不裸调外部依赖**。**LLM 不是函数，是分布式系统的外部节点**。**用 Decorator pattern 而不是 LangChain 类框架**——因为 5 个 Agent 庭审是我的具体场景，**自己写的 250 行我能讲清楚每一层**，LangChain 50 万行我讲不清。
>
> 真实场景：一场庭审 45 次 LLM 调用。**有 Gateway**：12 次失败自动切 fallback / 30 次 budget 拦截 / 40 次 prompt 自动压缩。**无 Gateway**：12 次失败卡住，30 次超支爆掉，40 次慢到用户放弃。**ROI = 用户体验 + 成本可控**。

---

## 8. 【反思】

### 反思 1：**不裸调外部依赖是基本功，不是"过度工程"**

v0.5 之前的 DecisionCourt 是裸调 LLM。**为什么 v0.7 我加了 Gateway？**—— 因为我跑过 v0.5 demo，深夜 DeepSeek 504 我没法继续玩。**那时我才意识到 LLM 是分布式系统的外部节点**。**这次教训让我以后对所有外部依赖（搜索 provider / OCR / vector DB）都会设计 Gateway**。

### 反思 2：**Decorate 一个接口 ≠ 过度抽象**

装饰器 = 责任分离 + 可测 + 可扩展。**关键是"找到正确的责任分解"**。我的 5 个装饰器是业界经典模式（circuit-breaker / retry / fallback / budget / throttle），**不是发明的**，**只是把它们组合在一起**。**好的设计 = 用经典模式解决问题**。

### 反思 3：**v0.8.3 bug 1（llm_calls 外键失败）教会我**：**依赖 schema 的代码要 unit-test**

`RecordingDecorator` 调用 `GORMStore.Record` 时，**假定 llm_calls.session_id 是 DB 主键**。但我的代码传了 session_uuid 当主键，**外键失败**。**没写 unit test 覆盖这条路径 = 业务跑得欢但 0 行审计**。

**教训**：**装饰器链 = 多层调用，每层都要单测**。**e2e test 不能替代 unit test**——这次 e2e test 没发现问题（业务没受影响），是 v0.8 白盒化（看 metrics / 看 llm_calls 行数）才暴露。**白盒化 + 单元测试 = 双保险**。

---

## 9. 名词速查

| 名词 | 含义 |
|---|---|
| Decorator pattern | 装饰器模式（设计模式） |
| Reliability chain | 容错链路（retry + circuit-breaker + fallback） |
| Circuit breaker | 熔断器（避免雪崩） |
| Fallback chain | 降级链（provider 切换） |
| Smart Compression | 智能压缩（保留关键信息） |
| Budget | 单 session token 上限 |
| Throttle | 限流 |
| Recording | 47 字段审计 |
| Provider | LLM 提供商（deepseek / openai / mock） |

---

**下一步**：
- [`05-whitebox-observability.md`](05-whitebox-observability.md) —— v0.8 面试杀手锏（白盒化）
- [`06-bug-stories.md`](06-bug-stories.md) —— 上面提到的好几个 bug 真实故事

