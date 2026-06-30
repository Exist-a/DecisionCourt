"use client";

/**
 * MemoryTimeline renders one row of the v0.5 episodic-memory audit panel.
 * Each row corresponds to one A2A private message authored by an Agent
 * (visibility=private, one of the four MemoryKind values).
 *
 * v0.5 升级：拆 3 种渲染路径，按数据完整度自动降级：
 *   1) **结构化卡片**（推荐）—— 后端填了 stance / confidence / reasoning
 *      字段，渲染成"立场 chip + 置信度条 + 推理段"的法庭记录风格。
 *   2) **纯文本** —— 老 a2a 消息或非 strategy_note 类型，只渲染 content。
 *   3) **真实法庭模式** —— redacted=true，content 隐藏，只显示 kind。
 *
 * 详见 .trae/documents/memory-a2a-redesign.md §1.5。
 */

import { Lightbulb, Swords, Undo2, Search } from "lucide-react";
import type { MemoryEntry, MemoryKind } from "@/types";

interface MemoryTimelineProps {
  entry: MemoryEntry;
  /** When true, hides entry.content + linkedEvidenceIds. */
  redacted: boolean;
}

interface KindStyle {
  label: string;
  Icon: typeof Lightbulb;
  /** Tailwind classes for the icon background + accent border. */
  chip: string;
  dot: string;
  /** 卡片左边框 + 背景 tint（让 4 种 kind 在 timeline 上一眼能区分）。 */
  card: string;
}

const KIND_STYLES: Record<MemoryKind, KindStyle> = {
  strategy_note: {
    label: "策略笔记",
    Icon: Lightbulb,
    chip: "bg-blue-50 text-blue-700 border-blue-200",
    dot: "bg-blue-500",
    card: "border-l-blue-400 bg-blue-50/30",
  },
  opponent_weakness: {
    label: "对方弱点",
    Icon: Swords,
    chip: "bg-rose-50 text-rose-700 border-rose-200",
    dot: "bg-rose-500",
    card: "border-l-rose-400 bg-rose-50/30",
  },
  self_correction: {
    label: "自我修正",
    Icon: Undo2,
    chip: "bg-amber-50 text-amber-700 border-amber-200",
    dot: "bg-amber-500",
    card: "border-l-amber-400 bg-amber-50/30",
  },
  evidence_eval: {
    label: "证据评估",
    Icon: Search,
    chip: "bg-emerald-50 text-emerald-700 border-emerald-200",
    dot: "bg-emerald-500",
    card: "border-l-emerald-400 bg-emerald-50/30",
  },
};

const AGENT_LABELS: Record<string, string> = {
  prosecutor: "控方",
  defender: "辩方",
  investigator: "调查员",
};

const STANCE_LABELS: Record<string, { label: string; tone: string }> = {
  pro_a: { label: "支持选项A", tone: "bg-prosecution/10 text-prosecution-ink border-prosecution/30" },
  pro_b: { label: "支持选项B", tone: "bg-defense/10 text-defense-ink border-defense/30" },
  challenge: { label: "质疑证据", tone: "bg-judge/10 text-judge-ink border-judge/30" },
};

function formatTime(iso: string): string {
  // Best-effort local-time formatting; falls back to the raw string on
  // parse failure so the UI never crashes on bad timestamps.
  try {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return iso;
    return d.toLocaleTimeString("zh-CN", {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: false,
    });
  } catch {
    return iso;
  }
}

/** 结构化字段是否齐全 —— 决定走"结构化卡片"还是"纯文本"。 */
function hasStructuredFields(entry: MemoryEntry): boolean {
  return (
    typeof entry.stance === "string" ||
    typeof entry.confidence === "number" ||
    typeof entry.reasoning === "string"
  );
}

export function MemoryTimeline({ entry, redacted }: MemoryTimelineProps) {
  const style = KIND_STYLES[entry.kind];
  const Icon = style.Icon;
  const agentLabel = AGENT_LABELS[entry.agentType] ?? entry.agentType;
  const isStructured = hasStructuredFields(entry);
  const stanceInfo = entry.stance ? STANCE_LABELS[entry.stance] : undefined;

  return (
    <li
      className="relative pl-7 pb-4 border-l border-rule last:border-l-transparent"
      data-memory-kind={entry.kind}
      data-memory-redacted={redacted ? "true" : "false"}
      data-memory-structured={isStructured ? "true" : "false"}
    >
      {/* timeline dot */}
      <span
        className={`absolute -left-[5px] top-3 w-2.5 h-2.5 rounded-full ring-2 ring-paper ${style.dot}`}
        aria-hidden="true"
      />

      <div
        className={`rounded-md border border-rule border-l-4 ${style.card} px-3 py-2.5`}
      >
        {/* header row: kind chip + agent + round + time */}
        <div className="flex items-center gap-2 flex-wrap text-[11px] font-data mb-1.5">
          <span
            className={`inline-flex items-center gap-1 px-1.5 py-0.5 border rounded-sm ${style.chip}`}
          >
            <Icon className="w-3 h-3" />
            {style.label}
          </span>
          <span className="text-inkSoft font-body">{agentLabel}</span>
          <span className="text-inkFaint">·</span>
          <span className="text-inkFaint">Round {entry.round}</span>
          <span className="text-inkFaint">·</span>
          <span className="text-inkFaint">{formatTime(entry.createdAt)}</span>
        </div>

        {/* body */}
        {redacted ? (
          <p className="text-[12px] text-inkFaint italic mt-1">
            （真实法庭模式：内容已隐藏）
          </p>
        ) : isStructured ? (
          <StructuredBody entry={entry} stanceInfo={stanceInfo} />
        ) : (
          <PlainBody entry={entry} />
        )}
      </div>
    </li>
  );
}

/**
 * StructuredBody 是 v0.5 新增的结构化卡片 —— 立场 chip / 置信度条 /
 * 推理段各自一行，比之前的 raw dump 直观得多。
 */
function StructuredBody({
  entry,
  stanceInfo,
}: {
  entry: MemoryEntry;
  stanceInfo?: { label: string; tone: string };
}) {
  const confidencePct =
    typeof entry.confidence === "number"
      ? Math.round(Math.max(0, Math.min(1, entry.confidence)) * 100)
      : null;

  return (
    <div className="flex flex-col gap-1.5">
      {/* 立场 + 置信度 一行 */}
      <div className="flex items-center gap-2 flex-wrap">
        {stanceInfo && (
          <span
            className={`inline-flex items-center px-1.5 py-0.5 border rounded-sm text-[11px] font-data ${stanceInfo.tone}`}
          >
            立场：{stanceInfo.label}
          </span>
        )}
        {confidencePct !== null && (
          <ConfidenceBar pct={confidencePct} />
        )}
      </div>

      {/* 推理 —— LLM 的"为什么持这个立场" */}
      {entry.reasoning && (
        <p className="text-[13px] text-ink leading-relaxed font-body">
          <span className="text-inkFaint text-[11px] font-data mr-1">推理</span>
          {entry.reasoning}
        </p>
      )}

      {/* 引用证据 */}
      {entry.linkedEvidenceIds.length > 0 && (
        <div className="flex items-center gap-1.5 flex-wrap text-[10px] font-data text-inkSoft mt-0.5">
          <span className="uppercase tracking-[0.15em] text-inkFaint">
            引用证据
          </span>
          {entry.linkedEvidenceIds.map((eid) => (
            <span
              key={eid}
              className="px-1.5 py-0.5 bg-paper border border-rule rounded-sm text-ink font-data"
            >
              {eid}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

/**
 * ConfidenceBar 用纯 Tailwind 渲染一条 0..100% 置信度条，比单纯一个数字
 * 直观。颜色按区间：>=80 深青 / 50-80 中性 / <50 灰。
 */
function ConfidenceBar({ pct }: { pct: number }) {
  const fillClass =
    pct >= 80
      ? "bg-defense"
      : pct >= 50
        ? "bg-judge"
        : "bg-inkFaint";
  return (
    <span className="inline-flex items-center gap-1.5 text-[11px] font-data text-inkSoft">
      <span>置信度</span>
      <span
        className="relative inline-block w-16 h-1.5 bg-paperDeep border border-rule rounded-full overflow-hidden"
        aria-label={`置信度 ${pct}%`}
      >
        <span
          className={`absolute inset-y-0 left-0 ${fillClass}`}
          style={{ width: `${pct}%` }}
        />
      </span>
      <span className="text-ink font-data tabular-nums">{pct}%</span>
    </span>
  );
}

/**
 * PlainBody 是 fallback —— 老 a2a 消息或非 strategy_note 类型没有结构化
 * 字段时，退回纯文本显示，仍然可用。
 */
function PlainBody({ entry }: { entry: MemoryEntry }) {
  return (
    <>
      <p className="text-[13px] text-ink leading-relaxed font-body whitespace-pre-wrap">
        {entry.content || "（笔记内容为空）"}
      </p>
      {entry.linkedEvidenceIds.length > 0 && (
        <div className="flex items-center gap-1.5 flex-wrap text-[10px] font-data text-inkSoft mt-1">
          <span className="uppercase tracking-[0.15em] text-inkFaint">
            引用证据
          </span>
          {entry.linkedEvidenceIds.map((eid) => (
            <span
              key={eid}
              className="px-1.5 py-0.5 bg-paperDeep border border-rule rounded-sm"
            >
              {eid}
            </span>
          ))}
        </div>
      )}
    </>
  );
}