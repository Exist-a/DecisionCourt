"use client";

// v0.6 BeliefTrajectoryTab: the right-sidebar tab that shows the
// full belief-diff timeline + the convergence reason (if any).
//
// Layout:
//   - Top: optional ConvergenceBadge (only visible after convergence fires)
//   - Body: chronological list of BeliefDiffCards, most recent on top.
//   - Empty state: hint pointing the user at Submit Evidence to see diffs.

import { useMemo, useState } from "react";
import { Inbox } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { BeliefDiffCard } from "./BeliefDiffCard";
import { ConvergenceBadge } from "./ConvergenceBadge";
import type { AgentType, BeliefDiff, ConvergenceInfo } from "@/types";

interface BeliefTrajectoryTabProps {
  diffs: BeliefDiff[];
  convergenceInfo: ConvergenceInfo | null;
}

const agentFilters: { value: AgentType | "all"; label: string }[] = [
  { value: "all", label: "全部" },
  { value: "prosecutor", label: "控方" },
  { value: "defender", label: "辩方" },
  { value: "judge", label: "法官" },
  { value: "investigator", label: "调查员" },
];

export function BeliefTrajectoryTab({
  diffs,
  convergenceInfo,
}: BeliefTrajectoryTabProps) {
  const [filter, setFilter] = useState<AgentType | "all">("all");

  const filtered = useMemo(() => {
    // Newest first for the UI; store keeps chronological order internally.
    const list = filter === "all" ? diffs : diffs.filter((d) => d.agent_type === filter);
    return [...list].sort(
      (a, b) =>
        b.round - a.round ||
        new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
    );
  }, [diffs, filter]);

  return (
    <div className="flex flex-col h-full">
      {/* Header: convergence badge (if any) + filter row */}
      <div className="px-3 py-2 border-b border-rule space-y-2 bg-paperDeep">
        {convergenceInfo && (
          <div className="flex items-center gap-2">
            <span className="text-[10px] uppercase tracking-wider text-inkFaint font-data">
              收敛原因
            </span>
            <ConvergenceBadge info={convergenceInfo} />
          </div>
        )}
        <div className="flex items-center gap-1 flex-wrap">
          <span className="text-[10px] uppercase tracking-wider text-inkFaint font-data mr-1">
            筛选
          </span>
          {agentFilters.map((f) => (
            <button
              key={f.value}
              onClick={() => setFilter(f.value)}
              className={`px-2 py-0.5 text-[11px] rounded-sm border transition-colors ${
                filter === f.value
                  ? "border-ink bg-ink text-paper"
                  : "border-rule text-inkSoft hover:border-inkSoft"
              }`}
              data-testid={`belief-filter-${f.value}`}
            >
              {f.label}
            </button>
          ))}
        </div>
      </div>

      {/* Body */}
      {filtered.length === 0 ? (
        <div className="flex-1 flex flex-col items-center justify-center px-6 text-center text-inkFaint">
          <Inbox className="w-8 h-8 mb-2 opacity-50" />
          <p className="text-sm">
            {diffs.length === 0
              ? "信念变化轨迹会显示在这里。提交一条证据后即可看到第一条 diff。"
              : "当前筛选下没有匹配的 diff。"}
          </p>
        </div>
      ) : (
        <ScrollArea className="flex-1">
          <div className="p-3 space-y-2" data-testid="belief-diff-list">
            {filtered.map((d) => (
              <BeliefDiffCard key={d.id} diff={d} />
            ))}
          </div>
        </ScrollArea>
      )}
    </div>
  );
}
