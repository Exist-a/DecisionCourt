"use client";

// v1.0-patch (2026-08-22) PR-2: 历史庭审列表 UI 组件
//
// 用途: 首页立案表单下方展示用户历史庭审 (localStorage + server 同步)。
// 行为:
//   - mount 时调 GET /api/v1/courtrooms 同步 server → localStorage
//   - 渲染 list (按 updatedAt DESC, 已排序)
//   - 每行: 标题 + optionA vs optionB + 当前阶段 chip + 3 按钮 (回看庭审 / 看判决书 / 删除)
//   - "回看庭审" → router.push(/court/<uuid>) — court page 已 hydrate (PR-1)
//   - "看判决书" → router.push(/verdict/<uuid>) — 只读 (verdict page 正常工作)
//   - "× 删除" → removeHistoryItem (localStorage 软删, DB 保留)

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { ChevronDown, ChevronRight, History, X, Scale, Gavel } from "lucide-react";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  loadHistory,
  removeHistoryItem,
  syncFromServer,
  type TrialHistoryItem,
} from "@/lib/trialHistory";
import { api } from "@/lib/api";

export function TrialHistoryList() {
  const router = useRouter();
  const [items, setItems] = useState<TrialHistoryItem[]>([]);
  const [collapsed, setCollapsed] = useState(false);

  // 启动: 调 server 同步, 失败回退 localStorage
  useEffect(() => {
    let mounted = true;
    void (async () => {
      // 1. 先读 localStorage 立即渲染 (避免 loading 闪烁)
      const local = loadHistory();
      if (mounted) setItems(local);

      // 2. 调 server 同步 (失败静默, localStorage 兜底)
      try {
        const res = await api.listMySessions();
        if (!mounted) return;
        if (res.code === 0 && Array.isArray(res.data.sessions)) {
          const synced = syncFromServer(res.data.sessions);
          setItems(synced);
        }
      } catch (err) {
        console.warn("[TrialHistoryList] server sync failed:", err);
        // localStorage 数据已显示, 失败 OK
      }
    })();
    return () => {
      mounted = false;
    };
  }, []);

  // 删除
  function handleRemove(uuid: string) {
    removeHistoryItem(uuid);
    setItems((prev) => prev.filter((x) => x.sessionUUID !== uuid));
  }

  // 看判决书 — 跳转 /verdict/<uuid>
  function handleOpenVerdict(uuid: string) {
    router.push(`/verdict/${uuid}`);
  }

  // 回看庭审现场 — 跳转 /court/<uuid> (court page mount 会自动 hydrate)
  function handleOpenCourt(uuid: string) {
    router.push(`/court/${uuid}`);
  }

  // 折叠态: items=0 直接不渲染整个面板 (空 UI 无意义)
  if (items.length === 0) return null;

  return (
    <section className="container mx-auto max-w-5xl px-6 pb-8">
      <Card className="bg-paperDeep border border-rule shadow-paper p-6">
        {/* 折叠头部 */}
        <button
          onClick={() => setCollapsed(!collapsed)}
          className="flex items-center gap-2 text-ink hover:text-inkSoft w-full"
          aria-expanded={!collapsed}
          data-testid="trial-history-toggle"
        >
          {collapsed ? (
            <ChevronRight className="w-4 h-4" />
          ) : (
            <ChevronDown className="w-4 h-4" />
          )}
          <History className="w-4 h-4 text-prosecution" />
          <h3 className="text-display text-sm font-semibold tracking-wider">
            历 史 庭 审
          </h3>
          <span className="text-[10px] text-inkFaint font-data ml-auto">
            {items.length} 条
          </span>
        </button>

        {!collapsed && (
          <div className="mt-4 space-y-2">
            {items.map((item) => (
              <div
                key={item.sessionUUID}
                className="bg-white border border-rule rounded-sm px-4 py-3 flex items-center gap-3 group"
                data-testid="trial-history-item"
              >
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <p className="text-sm text-display font-semibold text-ink truncate">
                      {item.title}
                    </p>
                    <PhaseChip
                      phase={item.currentPhase}
                      verdictReady={item.verdictReady}
                    />
                  </div>
                  <p className="text-[10px] text-inkFaint font-data mt-0.5">
                    {item.optionA} <span className="mx-1 text-inkFaint">⚖</span>{" "}
                    {item.optionB}
                  </p>
                </div>

                <div className="flex items-center gap-1 shrink-0">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => handleOpenCourt(item.sessionUUID)}
                    className="text-ink hover:bg-paper h-7 px-2 text-[10px] font-data tracking-wider"
                    data-testid="trial-history-open-court"
                  >
                    <Scale className="w-3 h-3 mr-1" />
                    回看庭审
                  </Button>
                  {item.verdictReady && (
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => handleOpenVerdict(item.sessionUUID)}
                      className="text-ink hover:bg-paper h-7 px-2 text-[10px] font-data tracking-wider"
                      data-testid="trial-history-open-verdict"
                    >
                      <Gavel className="w-3 h-3 mr-1" />
                      看判决书
                    </Button>
                  )}
                  <Button
                    size="icon"
                    variant="ghost"
                    onClick={() => handleRemove(item.sessionUUID)}
                    className="text-inkFaint hover:text-prosecution h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity"
                    aria-label="删除历史记录"
                    data-testid="trial-history-remove"
                  >
                    <X className="w-3.5 h-3.5" />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
    </section>
  );
}

function PhaseChip({
  phase,
  verdictReady,
}: {
  phase: TrialHistoryItem["currentPhase"];
  verdictReady: boolean;
}) {
  const label = phaseToLabel(phase);
  const className = verdictReady
    ? "bg-amber-900/30 text-amber-300 border-amber-700/40"
    : "bg-stone-700/40 text-stone-300 border-stone-600/40";
  return (
    <span
      className={`text-[9px] uppercase tracking-wider px-1.5 py-0.5 border rounded-sm font-data ${className}`}
    >
      {label}
    </span>
  );
}

function phaseToLabel(phase: TrialHistoryItem["currentPhase"]): string {
  switch (phase) {
    case "idle":
      return "立案";
    case "opening":
      return "开庭";
    case "cross_exam":
      return "质证";
    case "closing":
      return "辩论";
    case "deliberation":
      return "审议";
    case "verdict":
      return "判决";
    default:
      return String(phase);
  }
}
