"use client";

/**
 * MemoryAuditPanel is the v0.5 right-sidebar "策略笔记" tab. It renders
 * the agent's private episodic memory (the four A2A private MessageTypes)
 * in chronological order.
 *
 * The panel supports the "真实法庭模式" toggle: when ON, individual row
 * content is hidden — only kind + count summary are shown. The toggle is
 * a UI-only filter; it does NOT change what the backend stores or what
 * the LLM sees (so toggling mid-trial does not change agent behavior).
 *
 * Empty state: when there are no entries yet, the panel shows a hint
 * explaining that private notes are written after each ReAct reflect step.
 */

import { useMemo } from "react";
import { Brain, Eye, EyeOff } from "lucide-react";
import type { MemoryEntry, MemoryKind } from "@/types";
import { MemoryTimeline } from "./MemoryTimeline";

interface MemoryAuditPanelProps {
  /** All memory entries, already chronologically sorted (round asc + createdAt asc). */
  entries: MemoryEntry[];
  /** Global redaction toggle. true = "真实法庭模式". */
  redactedMode: boolean;
  /** Update the toggle (lifted to parent so verdict page can also control it). */
  onToggleRedacted: () => void;
}

const KIND_LABELS: Record<MemoryKind, string> = {
  strategy_note: "策略笔记",
  opponent_weakness: "对方弱点",
  self_correction: "自我修正",
  evidence_eval: "证据评估",
};

export function MemoryAuditPanel({
  entries,
  redactedMode,
  onToggleRedacted,
}: MemoryAuditPanelProps) {
  // Group counts so the empty/redacted state still shows something
  // informative ("策略笔记 × 3").
  const kindCounts = useMemo(() => {
    const counts: Record<MemoryKind, number> = {
      strategy_note: 0,
      opponent_weakness: 0,
      self_correction: 0,
      evidence_eval: 0,
    };
    for (const e of entries) {
      counts[e.kind] += 1;
    }
    return counts;
  }, [entries]);

  const totalCount = entries.length;

  return (
    <section
      className="flex flex-col h-full"
      data-component="MemoryAuditPanel"
      data-redacted-mode={redactedMode ? "true" : "false"}
    >
      {/* header: title + toggle */}
      <header className="px-3 py-2 border-b border-rule bg-paperDeep flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 text-[11px] font-data tracking-[0.15em] uppercase text-ink">
          <Brain className="w-3.5 h-3.5" />
          策略笔记
        </div>
        <button
          type="button"
          onClick={onToggleRedacted}
          aria-pressed={redactedMode}
          title={
            redactedMode
              ? "当前：真实法庭模式（隐藏律师内心戏）"
              : "当前：AI 可视化模式（显示完整笔记）"
          }
          data-testid="real-courthouse-toggle"
          className={`inline-flex items-center gap-1 px-2 py-1 border rounded-sm text-[10px] font-data tracking-wider transition-colors ${
            redactedMode
              ? "bg-seal text-paper border-seal hover:bg-seal-ink"
              : "bg-white text-inkSoft border-rule hover:border-inkSoft"
          }`}
        >
          {redactedMode ? (
            <>
              <EyeOff className="w-3 h-3" />
              真实法庭
            </>
          ) : (
            <>
              <Eye className="w-3 h-3" />
              AI 可视化
            </>
          )}
        </button>
      </header>

      {/* body: timeline or empty state */}
      <div className="flex-1 overflow-y-auto px-4 py-3">
        {totalCount === 0 ? (
          <EmptyState />
        ) : (
          <ul className="m-0 p-0 list-none" data-memory-list="true">
            {entries.map((entry) => (
              <MemoryTimeline
                key={entry.id}
                entry={entry}
                redacted={redactedMode}
              />
            ))}
          </ul>
        )}
      </div>

      {/* footer: kind 分布 —— 只显示有数据的 kind chip，让用户一眼看到比例 */}
      {totalCount > 0 && (
        <footer className="border-t border-rule px-3 py-2 bg-paperDeep">
          <div className="flex items-center gap-1.5 flex-wrap text-[10px] font-data text-inkFaint">
            {(Object.keys(kindCounts) as MemoryKind[]).map((k) => {
              const count = kindCounts[k];
              if (count === 0) return null;
              return (
                <span
                  key={k}
                  className="inline-flex items-center gap-1 px-1.5 py-0.5 bg-white border border-rule rounded-sm"
                  data-kind-count={k}
                >
                  <span className="text-ink">{KIND_LABELS[k]}</span>
                  <span className="text-inkSoft">× {count}</span>
                </span>
              );
            })}
          </div>
        </footer>
      )}
    </section>
  );
}

function EmptyState() {
  return (
    <div
      className="h-full flex flex-col items-center justify-center text-center text-inkFaint px-6"
      data-memory-empty="true"
    >
      <Brain className="w-8 h-8 mb-2 opacity-40" />
      <p className="text-[13px] font-body leading-relaxed">暂无策略笔记</p>
      <p className="text-[11px] font-data mt-1.5 leading-relaxed max-w-[220px]">
        律师在 ReAct 反思阶段会自动写下
        <br />
        策略笔记 / 对方弱点 / 自我修正 / 证据评估
      </p>
    </div>
  );
}
