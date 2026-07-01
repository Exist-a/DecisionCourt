"use client";

// v0.6 ConvergenceBadge: a small chip that appears next to the
// "trial.converged" status, showing *why* the trial ended early.
//
// The four reason codes map to four colors / icons:
//   - reasoning_oscillation: amber, Repeat icon (lawyer looped)
//   - consensus:             green, Handshake icon (both agreed)
//   - belief_stable:         blue,  Anchor icon (no movement)
//   - max_rounds:            stone, Clock icon (forced end)
//
// Hover reveals the human-readable Chinese caption from the backend.

import {
  Repeat,
  Handshake,
  Anchor as AnchorIcon,
  Clock,
  type LucideIcon,
} from "lucide-react";
import type { ConvergenceInfo } from "@/types";

interface ConvergenceBadgeProps {
  info: ConvergenceInfo;
}

const reasonMeta: Record<
  ConvergenceInfo["reason"],
  { label: string; color: string; Icon: LucideIcon }
> = {
  reasoning_oscillation: {
    label: "推理循环",
    color: "bg-amber-900/30 text-amber-300 border-amber-700/40",
    Icon: Repeat,
  },
  consensus: {
    label: "双方共识",
    color: "bg-emerald-900/30 text-emerald-300 border-emerald-700/40",
    Icon: Handshake,
  },
  belief_stable: {
    label: "信念稳定",
    color: "bg-sky-900/30 text-sky-300 border-sky-700/40",
    Icon: AnchorIcon,
  },
  max_rounds: {
    label: "已达最大轮次",
    color: "bg-stone-700/40 text-stone-300 border-stone-600/40",
    Icon: Clock,
  },
};

export function ConvergenceBadge({ info }: ConvergenceBadgeProps) {
  const meta = reasonMeta[info.reason];
  const { Icon } = meta;
  return (
    <div
      className={`inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs ${meta.color}`}
      title={info.reason_message}
      data-testid="convergence-badge"
    >
      <Icon className="w-3.5 h-3.5" />
      <span className="font-medium">{meta.label}</span>
      <span className="text-[10px] opacity-70">R{info.round}</span>
    </div>
  );
}
