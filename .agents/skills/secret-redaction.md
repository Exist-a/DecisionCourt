# Skill: secret-redaction

> **触发场景**：grep / cat 任何敏感文件（`.env` / 容器 env / docker inspect）后输出到对话

## 1. 红线（AGENTS.md §8）

**禁止回显**：
- `.env` 中所有 key 值（JWT_SECRET / DATABASE_URL / LLM_API_KEY / SEARCH_*_KEY 等）
- SSH 私钥内容
- 云厂商凭证（`.pem` / `*.key` / `service-account*.json`）

**禁止写入**：
- 任何其他文件（demo 脚本 / 测试 fixture / 文档示例 / commit message）
- commit message / PR 描述 / ADR 文档
- chat 输出（用 `<hidden>` 替代）

## 2. 4 类脱敏命令模板

### A. grep .env 关键项（值隐藏）

```bash
grep -E '^(JWT_SECRET|DATABASE_URL|LLM_API_KEY|BOCHA_API_KEY)' .env | sed 's/=.*/=<hidden>/'
```

输出：
```
JWT_SECRET=<hidden>
DATABASE_URL=<hidden>
LLM_API_KEY=<hidden>
```

### B. 显示 value 长度（用于诊断）

```bash
grep '^JWT_SECRET=' .env | sed 's/^JWT_SECRET=//' | tr -d '\n' | wc -c
```

输出：`88`（字节数，用于确认 value 是否非空）

### C. 容器 env 列出（值隐藏）

```bash
docker exec dc_backend env | grep -E '^(JWT_SECRET|DATABASE_URL|LLM_API_KEY)=' | sed 's/=.*/=<set, length=88+>/'
```

输出：
```
JWT_SECRET=<set, length=88+>
DATABASE_URL=<set, length=88+>
```

### D. 十六进制 dump（用于诊断特殊字符）

```bash
grep '^JWT_SECRET=' .env | head -1 | xxd | head -3
```

输出：
```
00000000: 4a57 545f 5345 4352 4554 3d6d 706b 424b  JWT_SECRET=mpkBK
```

**用途**：发现是否有引号 / 转义 / 不可见字符（base64 里 `+` `/` `=` 都是合法的，不会破坏）。

## 3. Docker compose config 输出处理

```bash
docker compose config | grep -E 'JWT_SECRET|DATABASE_URL|LLM_API_KEY' | sed 's/=.*$/=<hidden>/'
```

**⚠️ 注意**：`docker compose config` 输出含所有 env var value，**必须用 sed 隐藏**。

## 4. Docker inspect 输出处理

```bash
docker inspect dc_backend --format '{{range .Config.Env}}{{println .}}{{end}}' | grep JWT_SECRET | wc -c
```

**只输出字节数**（如 100），不输出 value。

## 5. 4 渠道诊断输出模板

完整诊断输出应该长这样（**全部脱敏**）：

```bash
=== .env keys (values hidden) ===
JWT_SECRET=<hidden>
DATABASE_URL=<hidden>
LLM_API_KEY=<hidden>

=== JWT_SECRET value length ===
88  # base64 64 chars + JWT_SECRET= prefix 12 bytes + newline = 88

=== container env (hidden) ===
JWT_SECRET=<set, length=88+>
DATABASE_URL=<set, length=88+>
LLM_API_KEY=<set, length=88+>

=== docker compose config (hidden) ===
JWT_SECRET: mpkBK...<hidden>
DATABASE_URL: postgres://...<hidden>

=== backend log ===
FATAL: required config JWT_SECRET is empty  # 即使是 backend log 也不能含 key value
```

## 6. 违规处理

如果不小心回显了 key value 到对话：
1. **立即停止当前任务**
2. 主动告知 user
3. 不要尝试"自己恢复"（Agent 不知道原 key）
4. 等待 user 手工重置 key + 评估影响范围

## 7. 关联文档

- [AGENTS.md §8 敏感文件红线](../../../AGENTS.md)
- [ADR 0025 §4.1 JWT_SECRET 影响范围](../../docs/adr/0025-security-p0-closeout.md)