"use client";

/**
 * BehindTheScenesPanel is the v0.5 "幕后视角" panel shown on the verdict
 * page after a trial ends. It wraps MemoryAuditPanel with a different
 * header (since this is a *retrospective* view, not a live feed) and
 * explicitly unlocks the full timeline — there is no "真实法庭模式"
 * redaction here, because the verdict page is post-trial and the user is
 * the judge / auditor.
 *
 * v0.5 design decision (memory-a2a-redesign.md §1.5):
 *   - Live trial: redacted mode is opt-in (toggle in right sidebar)
 *   - Post-trial verdict page: redaction is OFF (full history visible)
 *
 * This makes "幕后视角" a tangible reward for completing the trial:
 * users see what the lawyers were *really* thinking.
 */

import { Eye, Lock } from "lucide-react";
import { MemoryAuditPanel } from "./MemoryAuditPanel";
import type { MemoryEntry } from "@/types";

interface BehindTheScenesPanelProps {
  entries: MemoryEntry[];
  /** v0.8.3：post-trial toggle 也真正可切换（原版固定 false 造成 UX 像坏掉） */
  redactedMode: boolean;
  /** v0.8.3：post-trial toggle 点击 handler */
  onToggleRedacted: () => void;
}

export function BehindTheScenesPanel({ entries }: BehindTheScenesPanelProps) {
  // Post-trial redaction is always OFF — the user has earned the full view.
  // We pass no-op handlers because the toggle is hidden in this view.
  return (
    <section
      data-component="BehindTheScenesPanel"
      data-redacted-mode="false"
      className="bg-white border border-rule shadow-paper p-6 md:p-8 relative"
    >
      {/* Top accent: distinguishes from the main transcript above */}
      <div className="absolute top-0 left-0 right-0 h-px bg-seal" />

      <div className="flex items-baseline justify-between mb-5">
        <h3 className="text-display text-lg font-semibold text-ink flex items-center gap-2">
          <Eye className="w-4 h-4 text-seal" />
          幕 后 视 角
        </h3>
        <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data flex items-center gap-1">
          <Lock className="w-3 h-3" />
          庭审结束后自动解锁
        </span>
      </div>

      <p className="text-display text-[13px] text-inkSoft leading-relaxed mb-4">
        以下是双方律师在庭审过程中的完整私有策略笔记 —— 他们的&ldquo;内心戏&rdquo;。
        庭审进行时这些笔记默认可见；这里强制完整展示，作为可审计的决策记录。
      </p>

      <div className="border border-rule rounded-sm overflow-hidden bg-paper">
        <MemoryAuditPanel
          entries={entries}
          redactedMode={redactedMode}
          onToggleRedacted={onToggleRedacted}
        />
      </div>
    </section>
  );
}