"use client";

import { Scale } from "lucide-react";

interface PhaseGuideProps {
  text: string;
  tone?: "slate" | "amber" | "emerald";
}

/**
 * 案卷·印章 风格：阶段提示横幅
 * - 纸色背景 + 一条左侧签条（颜色按状态）
 * - serif 文字（"现已进入……"判决书感）
 * - 不再用 emoji 蓝/绿色块
 */
const toneStyles = {
  slate: {
    bg: "bg-paperDeep",
    bar: "bg-ink",
    ink: "text-inkSoft",
  },
  amber: {
    bg: "bg-amber-50",
    bar: "bg-judge",
    ink: "text-judge-ink",
  },
  emerald: {
    bg: "bg-emerald-50",
    bar: "bg-seal",
    ink: "text-seal-ink",
  },
};

export function PhaseGuide({
  text,
  tone = "slate",
}: PhaseGuideProps) {
  const style = toneStyles[tone];
  return (
    <div
      className={`relative ${style.bg} border-y border-rule`}
    >
      <div className="absolute left-0 top-0 bottom-0 w-1" style={{ backgroundColor: style.bar === "bg-seal" ? "#C8342A" : style.bar === "bg-judge" ? "#7A5C3F" : "#1A1815" }} />
      <div className="container mx-auto max-w-6xl px-4 py-2.5 flex items-center gap-2.5">
        <Scale className="w-3.5 h-3.5 text-inkSoft shrink-0" />
        <span className={`text-display text-sm ${style.ink} leading-snug`}>
          {text}
        </span>
      </div>
    </div>
  );
}
