"use client";

import { useState } from "react";
import type { Evidence, EvidenceType } from "@/types";
import { formatEvidenceID } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Label } from "@/components/ui/label";
import { Plus, AlertTriangle, FolderOpen } from "lucide-react";

interface EvidenceBoardProps {
  evidences: Evidence[];
  onSubmit: (content: string, type: EvidenceType) => void;
}

const evidenceTypeLabels: Record<EvidenceType, string> = {
  fact: "事实",
  data: "数据",
  expert_opinion: "专家意见",
  preference: "个人偏好",
  constraint: "约束条件",
};

/**
 * 案卷·印章 风格：证据像"案卷夹"
 * - 每个证据卡片左边有"卷宗签条"（颜色按角色区分）
 * - 圆形是右上的标识（不是头像）
 * - 不再用 chip 圆角，全部用直角 + 边框（案卷夹风格）
 */
const sourceColors: Record<string, { tab: string; ink: string }> = {
  user: { tab: "#B53A2E", ink: "#7A1F18" },
  web_search: { tab: "#2C5470", ink: "#16334A" },
  agent_question: { tab: "#7A5C3F", ink: "#3F2E1E" },
  clarification_answer: { tab: "#A89F8E", ink: "#5C564F" },
};

export function EvidenceBoard({ evidences, onSubmit }: EvidenceBoardProps) {
  const [content, setContent] = useState("");
  const [type, setType] = useState<EvidenceType>("fact");
  const [expanded, setExpanded] = useState(false);

  const handleSubmit = () => {
    if (!content.trim()) return;
    onSubmit(content.trim(), type);
    setContent("");
    setExpanded(false);
  };

  return (
    <div className="bg-paper border-t border-rule">
      <div className="container mx-auto max-w-6xl px-4 py-5">
        {/* 案卷标题行 */}
        <div className="flex items-baseline justify-between mb-3">
          <div className="flex items-baseline gap-3">
            <h3 className="text-display text-base font-semibold text-ink flex items-center gap-2">
              <FolderOpen className="w-4 h-4 text-judge" />
              案卷·证据
            </h3>
            <span className="text-xs text-inkFaint font-data">
              {evidences.length} 件
            </span>
          </div>
          <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
            Evidence File
          </span>
        </div>

        {evidences.length === 0 ? (
          <div className="text-center py-6 text-sm text-inkFaint border border-dashed border-rule rounded-sm">
            尚无证据归档 · 请在下方提交第一份证据
          </div>
        ) : (
          <div className="flex gap-3 overflow-x-auto pb-3">
            {evidences.map((evidence) => {
              const colorCfg = sourceColors[evidence.source] ?? sourceColors.user;
              return (
                <div
                  key={evidence.evidence_id}
                  className="evidence-folder flex-shrink-0 w-64 relative"
                  style={{ ["--folder-tab" as string]: colorCfg.tab }}
                >
                  {/* 卷宗签条 */}
                  <div className="flex items-center justify-between mb-2">
                    <span
                      className="text-display text-sm font-semibold"
                      style={{ color: colorCfg.ink }}
                    >
                      {formatEvidenceID(evidence.evidence_id)}
                    </span>
                    <span className="text-[10px] uppercase tracking-wider text-inkFaint font-data px-1.5 py-0.5 border border-rule rounded-sm">
                      {evidenceTypeLabels[evidence.type]}
                    </span>
                  </div>

                  {/* 证据内容 */}
                  <p className="text-xs text-ink leading-relaxed line-clamp-3 mb-2 text-display">
                    {evidence.content}
                  </p>

                  {/* 来源 + 状态 */}
                  <div className="flex items-center justify-between text-[10px] text-inkFaint">
                    <span className="font-data tracking-wider">
                      {evidence.source === "user"
                        ? "用户提交"
                        : evidence.source === "web_search"
                          ? "网络搜索"
                          : evidence.source}
                    </span>
                    {evidence.status === "challenged" && (
                      <span
                        className="flex items-center gap-0.5 font-medium"
                        style={{ color: colorCfg.tab }}
                      >
                        <AlertTriangle className="w-3 h-3" />
                        质疑
                      </span>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}

        {expanded ? (
          <div className="mt-3 space-y-3 p-4 bg-white border border-rule rounded-sm">
            <div className="space-y-2">
              <Label className="text-xs text-inkSoft font-data tracking-wider uppercase">
                证据内容
              </Label>
              <Textarea
                value={content}
                onChange={(e) => setContent(e.target.value)}
                placeholder="例如：我已存下 18 个月生活费的应急基金"
                className="bg-paper border border-rule rounded-sm min-h-[80px] resize-none text-display"
              />
            </div>
            <div className="space-y-2">
              <Label className="text-xs text-inkSoft font-data tracking-wider uppercase">
                证据类型
              </Label>
              <Select value={type} onValueChange={(v) => setType(v as EvidenceType)}>
                <SelectTrigger className="bg-paper border border-rule rounded-sm h-10">
                  <SelectValue>{evidenceTypeLabels[type]}</SelectValue>
                </SelectTrigger>
                <SelectContent className="bg-white border-rule">
                  {Object.entries(evidenceTypeLabels).map(([value, label]) => (
                    <SelectItem key={value} value={value}>
                      {label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex gap-2">
              <Button
                size="sm"
                onClick={handleSubmit}
                className="bg-ink text-paper hover:bg-inkSoft rounded-sm px-4"
              >
                归档证据
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => setExpanded(false)}
                className="border-rule text-inkSoft rounded-sm px-4"
              >
                取消
              </Button>
            </div>
          </div>
        ) : (
          <Button
            data-evidence-toggle
            variant="outline"
            size="sm"
            onClick={() => setExpanded(true)}
            className="mt-3 w-full border-dashed border-rule text-inkSoft hover:bg-paperDeep hover:border-inkSoft rounded-sm"
          >
            <Plus className="w-4 h-4 mr-2" />
            新增证据归档
          </Button>
        )}
      </div>
    </div>
  );
}
