"use client";

import type { BeliefSnapshot } from "@/types";
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from "recharts";

interface StanceChartProps {
  snapshots: BeliefSnapshot[];
}

export function StanceChart({ snapshots }: StanceChartProps) {
  const data = buildChartData(snapshots);

  return (
    <div className="h-[300px] w-full">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 10, right: 20, left: 0, bottom: 10 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#44403c" />
          <XAxis
            dataKey="round"
            stroke="#a8a29e"
            tickFormatter={(v) => `R${v}`}
          />
          <YAxis
            domain={[0, 1]}
            stroke="#a8a29e"
            tickFormatter={(v) => `${Math.round(v * 100)}%`}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: "#1c1917",
              border: "1px solid #44403c",
              borderRadius: "8px",
              color: "#f5f5f4",
            }}
            formatter={(value) =>
              typeof value === "number"
                ? `${Math.round(value * 100)}%`
                : String(value)
            }
          />
          <Legend wrapperStyle={{ color: "#d6d3d1" }} />
          <Line
            type="monotone"
            dataKey="prosecutor"
            name="控方支持 A"
            stroke="#f87171"
            strokeWidth={2}
            dot={{ r: 4, fill: "#f87171" }}
            activeDot={{ r: 6 }}
          />
          <Line
            type="monotone"
            dataKey="defender"
            name="辩方支持 A"
            stroke="#60a5fa"
            strokeWidth={2}
            dot={{ r: 4, fill: "#60a5fa" }}
            activeDot={{ r: 6 }}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}

function buildChartData(snapshots: BeliefSnapshot[]) {
  const rounds = Array.from(new Set(snapshots.map((s) => s.round))).sort(
    (a, b) => a - b
  );

  return rounds.map((round) => {
    const prosecutor = snapshots.find(
      (s) => s.round === round && s.agent_type === "prosecutor"
    );
    const defender = snapshots.find(
      (s) => s.round === round && s.agent_type === "defender"
    );

    return {
      round,
      prosecutor: prosecutor?.belief_a ?? 0.75,
      defender: defender?.belief_a ?? 0.25,
    };
  });
}
