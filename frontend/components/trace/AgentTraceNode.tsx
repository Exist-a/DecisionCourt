"use client";

// v1.0.4 PR-C2: AgentTraceNode — 单次 LLM 调用卡片
//
// 设计要点:
//   - 显示一次 LLM 调用的关键元数据 (agent_type / task_type / latency / tokens / status)
//   - 递归渲染 children (retry chain)
//   - 展开/折叠 prompt + output (默认折叠, 避免庭审页太长)
//
// 触发: TrialReplay 主组件按 tree 递归渲染每个节点
// 数据源: 后端 /api/v1/courtrooms/:uuid/traces/:trace_id 返回的 Trace.tree

import { useState } from "react";
import { ChevronDown, ChevronRight, AlertCircle, CheckCircle2 } from "lucide-react";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import type { TraceRunNode } from "@/types";

interface AgentTraceNodeProps {
  node: TraceRunNode;
  depth?: number;
}

const agentLabels: Record<string, string> = {
  prosecutor: "控方律师",
  defender: "辩方律师",
  investigator: "调查员",
  judge: "法官",
  clerk: "书记员",
};

const agentColors: Record<string, string> = {
  prosecutor: "bg-rose-900/30 text-rose-300 border-rose-700/40",
  defender: "bg-sky-900/30 text-sky-300 border-sky-700/40",
  investigator: "bg-violet-900/30 text-violet-300 border-violet-700/40",
  judge: "bg-amber-900/30 text-amber-300 border-amber-700/40",
  clerk: "bg-stone-700/40 text-stone-300 border-stone-600/40",
};

function formatLatency(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function formatTime(iso: string): string {
  // 显示 HH:MM:SS 便于庭审页对齐
  const d = new Date(iso);
  return d.toLocaleTimeString("zh-CN", { hour12: false });
}

export function AgentTraceNode({ node, depth = 0 }: AgentTraceNodeProps) {
  const [expanded, setExpanded] = useState(false);
  const { run, children } = node;
  const isError = run.status === "error";
  const isRetry = run.retry_count > 0;
  const hasChildren = children && children.length > 0;
  const agentLabel = agentLabels[run.agent_type] ?? run.agent_type;
  const agentColor = agentColors[run.agent_type] ?? "bg-stone-700/40 text-stone-300";

  return (
    <div className="space-y-2" style={{ marginLeft: depth * 16 }}>
      <Card className={`p-3 ${isError ? "border-red-700/60" : "border-stone-700/40"}`}>
        <div className="flex items-start justify-between gap-3">
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <Badge className={agentColor}>{agentLabel}</Badge>
              <span className="text-xs text-stone-400 font-mono">
                {run.task_type}
              </span>
              {isRetry && (
                <Badge variant="outline" className="text-amber-400 border-amber-700/40">
                  retry #{run.retry_count}
                </Badge>
              )}
              {isError ? (
                <AlertCircle className="w-4 h-4 text-red-400 shrink-0" />
              ) : (
                <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
              )}
            </div>

            <div className="mt-2 flex items-center gap-3 text-xs text-stone-400">
              <span className="font-mono">{formatTime(run.started_at)}</span>
              <span>→</span>
              <span className="font-mono">{formatLatency(run.latency_ms)}</span>
              <span className="text-stone-500">|</span>
              <span>{run.model}</span>
            </div>

            {isError && run.error_msg && (
              <div className="mt-2 text-xs text-red-400 font-mono break-words">
                {run.error_msg}
              </div>
            )}
          </div>

          {(hasChildren || run.error_msg) && (
            <button
              onClick={() => setExpanded(!expanded)}
              className="text-stone-400 hover:text-stone-200 shrink-0"
              aria-label={expanded ? "折叠" : "展开"}
            >
              {expanded ? (
                <ChevronDown className="w-4 h-4" />
              ) : (
                <ChevronRight className="w-4 h-4" />
              )}
            </button>
          )}
        </div>

        {expanded && (
          <div className="mt-3 pt-3 border-t border-stone-700/40 space-y-2 text-xs">
            <div className="text-stone-500">
              <span className="font-mono">run_id:</span> {run.run_id}
            </div>
            <div className="text-stone-500">
              <span className="font-mono">trace_id:</span> {run.trace_id}
            </div>
            {run.error_msg && (
              <pre className="bg-stone-900/60 p-2 rounded text-red-300 overflow-x-auto whitespace-pre-wrap break-words">
                {run.error_msg}
              </pre>
            )}
          </div>
        )}
      </Card>

      {hasChildren && (
        <div className="space-y-2">
          {children!.map((child, idx) => (
            <AgentTraceNode key={child.run.run_id ?? idx} node={child} depth={depth + 1} />
          ))}
        </div>
      )}
    </div>
  );
}
