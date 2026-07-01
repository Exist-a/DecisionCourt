"use client";

// v0.6 BeliefDiffCard: one-card-per-diff timeline entry.
//
// Renders a single (evidence, agent) Bayesian update step as a compact
// horizontal row:
//   [agent badge] [prior → posterior] [Δ sign + magnitude] [weight chip] [weaken badge]
// The "reason" text is shown below in muted type, truncated to one line.
//
// The component is intentionally small and dense — used inside
// BehindTheScenesPanel under a "信念变化时间线" tab so the user can
// see "为什么我的信念变了" without expanding a giant diff.

import { TrendingDown, TrendingUp, Minus, ShieldOff, Anchor } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { BeliefDiff, AgentType } from "@/types";

interface BeliefDiffCardProps {
  diff: BeliefDiff;
}

const agentLabels: Record<AgentType, string> = {
  prosecutor: "控方",
  defender: "辩方",
  investigator: "调查员",
  clerk: "书记员",
  judge: "法官",
};

const agentColors: Record<AgentType, string> = {
  prosecutor: "bg-rose-900/30 text-rose-300 border-rose-700/40",
  defender: "bg-sky-900/30 text-sky-300 border-sky-700/40",
  investigator: "bg-violet-900/30 text-violet-300 border-violet-700/40",
  clerk: "bg-stone-700/40 text-stone-300 border-stone-600/40",
  judge: "bg-amber-900/30 text-amber-300 border-amber-700/40",
};

const sourceLabels = {
  evidence: "evidence",
  weaken: "weaken",
  anchor_pull: "anchor",
} as const;

const sourceIcons = {
  evidence: TrendingUp,
  weaken: ShieldOff,
  anchor_pull: Anchor,
} as const;

function directionArrow(delta: number) {
  if (delta > 0) return <TrendingUp className="w-3 h-3 text-rose-400" />;
  if (delta < 0) return <TrendingDown className="w-3 h-3 text-sky-400" />;
  return <Minus className="w-3 h-3 text-stone-500" />;
}

export function BeliefDiffCard({ diff }: BeliefDiffCardProps) {
  const SourceIcon = sourceIcons[diff.source];
  const pct = (v: number) => `${(v * 100).toFixed(0)}%`;
  const deltaSign = diff.delta_belief_a > 0 ? "+" : "";
  // Only show the source icon when it's not the default (evidence) — keeps
  // the visual noise down for the 95% of diffs that ARE plain evidence.
  const showSource = diff.source !== "evidence";

  return (
    <div
      className="rounded-md border border-stone-700/40 bg-stone-900/40 p-2.5 hover:border-stone-600/60 transition-colors"
      data-testid="belief-diff-card"
    >
      <div className="flex items-center gap-2 flex-wrap text-xs">
        {/* Agent badge */}
        <Badge
          variant="outline"
          className={`px-1.5 py-0 h-5 text-[10px] ${agentColors[diff.agent_type]}`}
        >
          {agentLabels[diff.agent_type]}
        </Badge>

        {/* Prior → Posterior */}
        <span className="font-mono text-stone-300">
          {pct(diff.prior_belief_a)}
        </span>
        <span className="text-stone-600">→</span>
        <span className="font-mono text-stone-100 font-semibold">
          {pct(diff.posterior_belief_a)}
        </span>

        {/* Δ with direction arrow */}
        <span className="flex items-center gap-0.5 text-stone-400">
          {directionArrow(diff.delta_belief_a)}
          <span className="font-mono">
            {deltaSign}
            {(diff.delta_belief_a * 100).toFixed(1)}%
          </span>
        </span>

        {/* Weight chip (only when meaningfully large) */}
        {diff.evidence_weight >= 0.01 && (
          <span className="text-stone-500 text-[10px]">
            w={diff.evidence_weight.toFixed(2)}
          </span>
        )}

        {/* Weaken factor: only show when actively weakened */}
        {diff.weaken_factor < 0.99 && (
          <Badge
            variant="outline"
            className="px-1.5 py-0 h-5 text-[10px] bg-amber-900/20 text-amber-300 border-amber-700/40"
            title="此条证据被对方律师质疑（weaken 边）"
          >
            ×{diff.weaken_factor.toFixed(2)}
          </Badge>
        )}

        {/* Optional source icon (non-evidence sources) */}
        {showSource && (
          <span className="text-stone-500" title={sourceLabels[diff.source]}>
            <SourceIcon className="w-3 h-3" />
          </span>
        )}

        {/* Round tag */}
        <span className="ml-auto text-stone-600 text-[10px]">
          R{diff.round} · {diff.phase}
        </span>
      </div>

      {/* Reason text — one line, truncated */}
      {diff.reason && (
        <p
          className="mt-1.5 text-[11px] text-stone-500 truncate"
          title={diff.reason}
        >
          {diff.reason}
        </p>
      )}
    </div>
  );
}
