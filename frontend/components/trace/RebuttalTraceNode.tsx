"use client";

// v1.0.4 PR-C2: RebuttalTraceNode — 反驳链展示
//
// 设计要点:
//   - 复用现有 /rebuttal-links 端点 (v1.0.2 候选 4 实装)
//   - 按 evidence_id 分组,每个 evidence 一个反驳链卡片
//   - 状态颜色: standing=红 (被反驳有效) / overturned=绿 (被翻盘) / withdrawn=灰
//   - 简化展示: 不画箭头,直接列出"被反驳的 evidence + 谁反驳的 + rationale"
//
// 复用 lib/rebuttal.ts fetchRebuttalLinks + useRebuttalCounts hook (已有)

import { useEffect, useState } from "react";
import { Swords, ShieldCheck, ShieldOff } from "lucide-react";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import type { RebuttalLink } from "@/types";
import { fetchRebuttalLinks } from "@/lib/rebuttal";

interface RebuttalTraceNodeProps {
  sessionUUID: string;
}

const useMock = process.env.NEXT_PUBLIC_USE_MOCK === "true";

const statusColors: Record<string, string> = {
  standing: "bg-rose-900/30 text-rose-300 border-rose-700/40",
  overturned: "bg-emerald-900/30 text-emerald-300 border-emerald-700/40",
  withdrawn: "bg-stone-700/40 text-stone-300 border-stone-600/40",
};

const statusLabels: Record<string, string> = {
  standing: "反驳有效",
  overturned: "已翻盘",
  withdrawn: "撤回",
};

export function RebuttalTraceNode({ sessionUUID }: RebuttalTraceNodeProps) {
  const [links, setLinks] = useState<RebuttalLink[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (useMock) {
      setLoading(false);
      return;
    }
    let cancelled = false;
    void fetchRebuttalLinks(sessionUUID).then((result) => {
      if (cancelled) return;
      setLinks(result);
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [sessionUUID]);

  if (loading) {
    return <div className="text-xs text-stone-500 py-2">加载反驳链...</div>;
  }
  if (links.length === 0) {
    return (
      <div className="text-xs text-stone-500 py-2">
        该庭审无 rebuttal 记录 (候选 4 在 cross_exam 阶段才会触发)
      </div>
    );
  }

  // 按 rebutted_evidence_id 分组
  const groups = new Map<string, RebuttalLink[]>();
  for (const link of links) {
    const arr = groups.get(link.rebutted_evidence_id) ?? [];
    arr.push(link);
    groups.set(link.rebutted_evidence_id, arr);
  }

  return (
    <div className="space-y-2">
      {Array.from(groups.entries()).map(([evidenceID, groupLinks]) => {
        const standing = groupLinks.filter((l) => l.status === "standing").length;
        const overturned = groupLinks.filter((l) => l.status === "overturned").length;
        const dominant = standing > 0 ? "standing" : overturned > 0 ? "overturned" : "withdrawn";
        const DominantIcon =
          dominant === "standing" ? Swords : dominant === "overturned" ? ShieldCheck : ShieldOff;

        return (
          <Card key={evidenceID} className="p-3 border-stone-700/40">
            <div className="flex items-center gap-2 mb-2">
              <DominantIcon className="w-4 h-4 text-stone-300" />
              <span className="text-xs text-stone-400 font-mono">
                evidence: {evidenceID.slice(0, 8)}
              </span>
              <Badge className={statusColors[dominant]}>
                {statusLabels[dominant]}
              </Badge>
            </div>

            <ul className="space-y-1">
              {groupLinks.map((link) => (
                <li key={link.id} className="text-xs text-stone-400 flex items-start gap-2">
                  <Badge variant="outline" className={statusColors[link.status] ?? ""}>
                    {link.aggressor_agent}
                  </Badge>
                  <span className="flex-1 break-words">
                    {link.rationale || <em className="text-stone-500">(无理由)</em>}
                  </span>
                </li>
              ))}
            </ul>
          </Card>
        );
      })}
    </div>
  );
}
