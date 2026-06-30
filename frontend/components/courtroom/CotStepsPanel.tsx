"use client";

import { useState } from "react";
import type { CotStep } from "@/types";

interface CotStepsPanelProps {
  steps: CotStep[];
}

/**
 * CotStepsPanel renders a ReAct lawyer's chain-of-thought in folded
 * form (per design choice B):
 *   - Action and Observation are expanded by default
 *   - Thought is collapsed behind a click-to-expand toggle
 *
 * The component is read-only and intentionally lightweight: each step is
 * one row, so a 4-iteration ReAct session takes ~4 rows even with full
 * thoughts visible. Visual language follows the existing MessageHistory
 * dossier aesthetic — paper tones, serif content, no chrome.
 */
export function CotStepsPanel({ steps }: CotStepsPanelProps) {
  const [openThought, setOpenThought] = useState<Set<number>>(new Set());
  const [expandedAll, setExpandedAll] = useState(false);

  if (!steps || steps.length === 0) {
    return null;
  }

  function toggleThought(idx: number) {
    setOpenThought((prev) => {
      const next = new Set(prev);
      if (next.has(idx)) next.delete(idx);
      else next.add(idx);
      return next;
    });
  }

  return (
    <div className="mt-2.5 border-t border-rule pt-2">
      <div className="flex items-baseline justify-between mb-1.5">
        <span className="text-[10px] uppercase tracking-[0.18em] text-inkFaint font-data">
          推理过程 · {steps.length} 步
        </span>
        <button
          type="button"
          onClick={() => setExpandedAll((v) => !v)}
          className="text-[10px] text-inkSoft hover:text-ink underline-offset-2 hover:underline"
        >
          {expandedAll ? "折叠全部思考" : "展开全部思考"}
        </button>
      </div>

      <ol className="space-y-1.5">
        {steps.map((step, idx) => {
          const isThoughtOpen = expandedAll || openThought.has(step.index);
          const isToolCall = step.action === "tool_call";
          const summary = summarizeStep(step);

          return (
            <li
              key={step.index}
              className="text-[12px] text-ink leading-relaxed"
            >
              {/* 头部：Step N + 摘要 */}
              <div className="flex items-baseline gap-2">
                <span className="font-data text-[10px] px-1.5 py-0.5 bg-paperDeep border border-rule rounded-sm text-inkSoft shrink-0">
                  Step {step.index + 1}
                </span>
                <span className="text-display text-[12px] text-ink">
                  {summary}
                </span>
              </div>

              {/* Thought（折叠 / 展开） */}
              {step.thought && (
                <div className="mt-1 pl-1">
                  <button
                    type="button"
                    onClick={() => toggleThought(step.index)}
                    className="text-[10px] text-inkSoft hover:text-ink inline-flex items-center gap-1"
                  >
                    <span className="font-data">
                      {isThoughtOpen ? "▾" : "▸"}
                    </span>
                    <span className="underline-offset-2 hover:underline">
                      思考过程
                    </span>
                  </button>
                  {isThoughtOpen && (
                    <p className="mt-1 text-[11px] text-inkSoft italic text-display border-l-2 border-rule pl-2 ml-1">
                      {step.thought}
                    </p>
                  )}
                </div>
              )}

              {/* 错误：tool 调用失败时显示 */}
              {step.error && !isToolCall && (
                <div className="mt-1 text-[10px] text-red-700">⚠ {step.error}</div>
              )}
            </li>
          );
        })}
      </ol>
    </div>
  );
}

// summarizeStep produces the one-line default-visible summary for each step.
// tool_call(investigator_search) → "🔍 调查发现 · 查询：<query>"
// tool_call(other)               → "调用 <tool>"
// speak                          → "完成发言"
// reflect                        → "纯反思 / 暂不发言"
// anything else                  → fallback to action string
function summarizeStep(step: CotStep): string {
  if (step.action === "tool_call") {
    const tool = step.tool_name ?? "tool";
    if (tool === "investigator_search") {
      const query =
        (step.tool_input?.query as string | undefined) ?? "(未提供 query)";
      const truncated = query.length > 40 ? `${query.slice(0, 40)}…` : query;
      // 调查发现 ≠ 用户证据：标签用「调查发现」以避免与 Evidence 列表混淆
      return `🔍 调查发现 · 查询：${truncated}`;
    }
    return `调用 ${tool}`;
  }
  if (step.action === "speak") {
    return "完成最终发言";
  }
  if (step.action === "reflect") {
    return "纯反思 · 暂不发言";
  }
  return step.action;
}