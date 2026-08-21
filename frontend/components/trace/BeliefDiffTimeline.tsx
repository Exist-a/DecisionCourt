"use client";

// v1.0.4 PR-C2: BeliefDiffTimeline — 信念变化曲线
//
// 设计要点:
//   - 从 v0.6 belief_diffs 表读 belief snapshots (通过现有 /belief-diffs 端点)
//   - 横轴 round / time,纵轴 belief_a 0-1
//   - 复用项目已有的 recharts ^3.9.0 (在 StanceChart 已使用)
//   - 多线: 控方/辩方/调查员/法官 各自 belief_a 走势
//
// 复用 lib/api.ts getBeliefDiffs (与 BehindTheScenesPanel 同源)
// 当前 trial 完成后 verdict 页加载;不在庭审中实时刷新 (后台分析)

import { useEffect, useState } from "react";
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
  CartesianGrid,
  ResponsiveContainer,
} from "recharts";
import type { BeliefDiff } from "@/types";
import { getAuthToken } from "@/lib/auth";

interface BeliefDiffTimelineProps {
  sessionUUID: string;
}

const useMock = process.env.NEXT_PUBLIC_USE_MOCK === "true";

interface DataPoint {
  round: number;
  prosecutor: number;
  defender: number;
  investigator: number;
  judge: number;
}

const agentColors: Record<string, string> = {
  prosecutor: "#fb7185", // rose-400
  defender: "#38bdf8", // sky-400
  investigator: "#a78bfa", // violet-400
  judge: "#fbbf24", // amber-400
};

export function BeliefDiffTimeline({ sessionUUID }: BeliefDiffTimelineProps) {
  const [data, setData] = useState<DataPoint[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (useMock) {
      setLoading(false);
      return;
    }
    let cancelled = false;
    const baseUrl = process.env.NEXT_PUBLIC_API_URL || "";
    const token = getAuthToken();
    fetch(`${baseUrl}/api/v1/courtrooms/${sessionUUID}/belief-diffs`, {
      method: "GET",
      credentials: "include",
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
      .then((r) => r.ok ? r.json() : Promise.reject(`status ${r.status}`))
      .then((json: { code: number; data?: { diffs?: BeliefDiff[] } }) => {
        if (cancelled) return;
        const diffs = json.data?.diffs ?? [];
        // 聚合成 { round, prosecutor, defender, investigator, judge } 序列
        const byRound = new Map<number, Partial<DataPoint>>();
        for (const d of diffs) {
          const existing = byRound.get(d.round) ?? { round: d.round };
          // posterior_belief_a 是更新后的值,直接采样
          if (d.agent_type in agentColors) {
            (existing as Record<string, number>)[d.agent_type] = d.posterior_belief_a;
          }
          byRound.set(d.round, existing);
        }
        const points = Array.from(byRound.values())
          .map((p) => ({
            round: p.round ?? 0,
            prosecutor: p.prosecutor ?? 0.5,
            defender: p.defender ?? 0.5,
            investigator: p.investigator ?? 0.5,
            judge: p.judge ?? 0.5,
          }))
          .sort((a, b) => a.round - b.round);
        setData(points);
        setLoading(false);
      })
      .catch((err) => {
        console.warn("[BeliefDiffTimeline] fetch failed:", err);
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [sessionUUID]);

  if (loading) {
    return <div className="text-xs text-stone-500 py-2">加载信念曲线...</div>;
  }
  if (data.length === 0) {
    return (
      <div className="text-xs text-stone-500 py-2">
        该庭审无 belief_diff 数据 (可能在 opening 阶段就结束了)
      </div>
    );
  }

  return (
    <ResponsiveContainer width="100%" height={200}>
      <LineChart data={data} margin={{ top: 8, right: 16, bottom: 8, left: 0 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="#44403c" />
        <XAxis
          dataKey="round"
          stroke="#a8a29e"
          fontSize={10}
          label={{ value: "round", position: "insideBottom", offset: -4, fill: "#a8a29e", fontSize: 10 }}
        />
        <YAxis
          domain={[0, 1]}
          stroke="#a8a29e"
          fontSize={10}
          label={{ value: "belief_a", angle: -90, position: "insideLeft", fill: "#a8a29e", fontSize: 10 }}
        />
        <Tooltip
          contentStyle={{ backgroundColor: "#1c1917", border: "1px solid #44403c" }}
          labelStyle={{ color: "#e7e5e4" }}
        />
        <Legend wrapperStyle={{ fontSize: 11 }} />
        <Line type="monotone" dataKey="prosecutor" stroke={agentColors.prosecutor} dot={false} strokeWidth={1.5} name="控方" />
        <Line type="monotone" dataKey="defender" stroke={agentColors.defender} dot={false} strokeWidth={1.5} name="辩方" />
        <Line type="monotone" dataKey="investigator" stroke={agentColors.investigator} dot={false} strokeWidth={1.5} name="调查员" />
        <Line type="monotone" dataKey="judge" stroke={agentColors.judge} dot={false} strokeWidth={1.5} name="法官" />
      </LineChart>
    </ResponsiveContainer>
  );
}
