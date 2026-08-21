"use client";

// v1.0.4 PR-C2: TrialReplay — 庭审回放时间轴主组件
//
// 设计要点:
//   - Dialog 包装 (复用 @/components/ui/dialog)
//   - 左侧 trace 列表 (按 StartedAt 升序) + 右侧选中 trace 详情 (含 tree)
//   - Tab 切换: LLM Trace / 信念曲线 / 反驳链
//   - 数据从 lib/trace.ts 的 fetchTraces / fetchTrace 拉
//   - 复用 AgentTraceNode (树状渲染) + BeliefDiffTimeline + RebuttalTraceNode
//
// 数据流:
//   useTraces(sessionUUID, date) → traces[] → 左侧列表
//   fetchTrace(sessionUUID, traceID) → 详情 (含 tree) → AgentTraceNode 渲染

import { useState } from "react";
import { Activity, TrendingUp, Swords } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { AgentTraceNode } from "./AgentTraceNode";
import { BeliefDiffTimeline } from "./BeliefDiffTimeline";
import { RebuttalTraceNode } from "./RebuttalTraceNode";
import type { Trace } from "@/types";
import { useTraces, fetchTrace } from "@/lib/trace";

interface TrialReplayProps {
  sessionUUID: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

function formatDuration(startIso: string, endIso: string): string {
  const ms = new Date(endIso).getTime() - new Date(startIso).getTime();
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

export function TrialReplay({ sessionUUID, open, onOpenChange }: TrialReplayProps) {
  const { traces, loading } = useTraces(sessionUUID, "");
  const [selectedTraceID, setSelectedTraceID] = useState<string | null>(null);
  const [traceDetail, setTraceDetail] = useState<Trace | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);

  // 自动选第一个 trace
  if (selectedTraceID === null && traces.length > 0) {
    setSelectedTraceID(traces[0].trace_id);
  }

  // 选中 trace 时拉详情
  if (selectedTraceID && (!traceDetail || traceDetail.trace_id !== selectedTraceID)) {
    setDetailLoading(true);
    void fetchTrace(sessionUUID, selectedTraceID).then((t) => {
      setTraceDetail(t);
      setDetailLoading(false);
    });
  }

  function handleSelectTrace(traceID: string) {
    setSelectedTraceID(traceID);
    setTraceDetail(null); // 触发重新拉
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-5xl max-h-[85vh] bg-stone-950 border-stone-700">
        <DialogHeader>
          <DialogTitle className="text-stone-100">庭审回放 (v1.0.4)</DialogTitle>
          <DialogDescription className="text-stone-400">
            按时间轴回顾所有 LLM 调用,展开 trace 看 prompt/output 详情
          </DialogDescription>
        </DialogHeader>

        <div className="grid grid-cols-[200px_1fr] gap-3 mt-2 min-h-[400px]">
          {/* 左侧 trace 列表 */}
          <ScrollArea className="border border-stone-800 rounded">
            <div className="p-2 space-y-1">
              {loading && (
                <div className="text-xs text-stone-500 p-2">加载 trace 列表...</div>
              )}
              {!loading && traces.length === 0 && (
                <div className="text-xs text-stone-500 p-2">
                  无 trace 数据 (今天还没 LLM 调用,或日志文件缺失)
                </div>
              )}
              {traces.map((t) => {
                const isSelected = t.trace_id === selectedTraceID;
                return (
                  <button
                    key={t.trace_id}
                    onClick={() => handleSelectTrace(t.trace_id)}
                    className={`w-full text-left p-2 rounded text-xs hover:bg-stone-800 ${
                      isSelected ? "bg-stone-800" : ""
                    }`}
                  >
                    <div className="text-stone-200 font-mono truncate">
                      {t.trace_id.slice(0, 12)}
                    </div>
                    <div className="text-stone-500 mt-1">
                      {t.runs.length} runs · {formatDuration(t.started_at, t.ended_at)}
                    </div>
                    {t.runs.some((r) => r.status === "error") && (
                      <Badge variant="outline" className="mt-1 text-red-400 border-red-700/40">
                        error
                      </Badge>
                    )}
                  </button>
                );
              })}
            </div>
          </ScrollArea>

          {/* 右侧详情 */}
          <div className="min-w-0">
            <Tabs defaultValue="trace" className="w-full">
              <TabsList>
                <TabsTrigger value="trace" className="text-xs">
                  <Activity className="w-3 h-3 mr-1" />
                  LLM Trace
                </TabsTrigger>
                <TabsTrigger value="belief" className="text-xs">
                  <TrendingUp className="w-3 h-3 mr-1" />
                  信念曲线
                </TabsTrigger>
                <TabsTrigger value="rebuttal" className="text-xs">
                  <Swords className="w-3 h-3 mr-1" />
                  反驳链
                </TabsTrigger>
              </TabsList>

              <TabsContent value="trace" className="mt-3">
                <ScrollArea className="h-[500px] pr-3">
                  {detailLoading && (
                    <div className="text-xs text-stone-500">加载 trace 详情...</div>
                  )}
                  {!detailLoading && !traceDetail && (
                    <Card className="p-4 text-xs text-stone-500 border-stone-700/40">
                      选中左侧 trace 查看详情
                    </Card>
                  )}
                  {!detailLoading && traceDetail && (
                    <AgentTraceNode node={traceDetail.tree} />
                  )}
                </ScrollArea>
              </TabsContent>

              <TabsContent value="belief" className="mt-3">
                <Card className="p-4 border-stone-700/40">
                  <BeliefDiffTimeline sessionUUID={sessionUUID} />
                </Card>
              </TabsContent>

              <TabsContent value="rebuttal" className="mt-3">
                <ScrollArea className="h-[500px] pr-3">
                  <RebuttalTraceNode sessionUUID={sessionUUID} />
                </ScrollArea>
              </TabsContent>
            </Tabs>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
