# ECS 资源评估（2026-08-05）

| | |
|---|---|
| **版本** | v1.0 |
| **生成日期** | 2026-08-05 |
| **基于** | [production-retrospective-2026-08-05.md §2 + §6](./production-retrospective-2026-08-05.md) |
| **建议结论** | **不升配 4C4G**，**加 2GB swap**（保底防 OOM） |

---

## 1. 当前资源使用（30 天沉淀数据）

| 指标 | 当前 | 占用 | 利用率 | 评估 |
|---|---|---|---|---|
| **内存** | 1.575 GiB | ~120 MiB（5 容器）| **7.6%** | 极度充足 |
| **磁盘** | 40 GB | 8.6 GB（清理后）| **23%** | 充足 |
| **Swap** | 0 B | — | — | **隐患**（OOM 即进程被杀）|
| **CPU** | 推测 2C | 几乎空载 | < 5% | 充足 |
| **网络** | 香港 ECS 出入 | < 1 Mbps | — | 充足 |

**业务压力**：30 天 17 场 trial / 227 次 LLM 调用 / 平均 3.74s/调用 / P99 12.90s。

---

## 2. 升配 vs 加 swap 对比

| 方案 | 成本（阿里云香港） | 收益 | 风险 | 决策 |
|---|---|---|---|---|
| **升 4C4G** | ~120 CNY/月（vs 当前 ~60 CNY）| 翻倍内存，但**用不满**（7.6% 利用率）| 无 | ❌ 不推荐 |
| **加 2GB swap** | 0（本地磁盘划 2GB）| OOM 时避免进程被杀；长期看也用不到 | swap 频繁使用 = 性能下降（但本项目极低概率）| ✅ 推荐 |
| **维持现状（不升不加）** | 0 | 节省成本 | OOM 时全栈崩溃 | ⚠️ 短期可接受 |

---

## 3. 加 swap 操作步骤（推荐执行）

```bash
# 1. 创建 2GB swap 文件
sudo fallocate -l 2G /swapfile
sudo chmod 600 /swapfile
sudo mkswap /swapfile
sudo swapon /swapfile

# 2. 永久生效（写入 /etc/fstab）
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab

# 3. 调低 swappiness（推荐 10，ECS 上避免过度 swap）
sudo sysctl vm.swappiness=10
echo 'vm.swappiness=10' | sudo tee -a /etc/sysctl.conf

# 4. 验证
free -h
# 输出应包含: Swap: 2.0G
```

预计时间：5 分钟
预计风险：极低（fallocate 不影响现有数据）

---

## 4. 何时需要真正升配

仅当以下任意条件触发：

| 触发条件 | 当前值 | 升配阈值 | 备注 |
|---|---|---|---|
| 5 容器内存 > 70% | 7.6% | 70% | 业务量翻 10 倍才会触发 |
| 磁盘使用 > 80% | 23% | 80% | 留 3-6 个月监控 |
| LLM P99 latency > 30s | 12.90s | 30s | 通常是 LLM provider 问题，不是 ECS |
| 真实 trial 数 / 月 > 100 | 17/月 | 100/月 | 业务增长信号 |

---

## 5. 结论

**当前规格（推测 2C2G）满足 6-12 个月业务需求**。

**唯一强烈建议**：加 2GB swap（5 分钟操作，0 成本，消除 OOM 单点风险）。

**长期**（> 6 个月）：根据实际 trial 数决定是否升 4C4G。

---

## 6. 关联文档

- [production-retrospective-2026-08-05.md §2 资源全景 + §6.1 .env.example 错位](./production-retrospective-2026-08-05.md)
- [ECS-MIGRATION-CHECKLIST.md §0 决策依据](./ECS-MIGRATION-CHECKLIST.md)
- [ACTION-ITEMS-ECS-EXPIRY-2026-08.md §D2](./ACTION-ITEMS-ECS-EXPIRY-2026-08.md)