"use client";

import { useEffect, useMemo, useRef } from "react";
import type { Agent } from "@/types";
import ReactFlow, {
  ReactFlowProvider,
  type Edge,
  type Node,
  Position,
} from "reactflow";
import "reactflow/dist/style.css";

// 模块级常量（必须）— 避免 ReactFlow nodeTypes/edgeTypes 警告
const EMPTY_NODE_TYPES = {};
const EMPTY_EDGE_TYPES = {};

interface ArgumentMapProps {
  optionA: string;
  optionB: string;
  agents: Agent[];
}

/**
 * 案卷·印章 风格：意见地图
 * - 配色：绛红 #B53A2E（控方）/ 深青 #2C5470（辩方）/ 棕金 #7A5C3F（法官）
 *   不用默认的 rose-500/blue-500/purple-500
 * - 连线：克制细线（strokeWidth 1~3），无杂多动画（去掉 animated dashed）
 * - 容器：纸色边框 + paper 阴影
 */
const agentColors: Record<
  string,
  { bg: string; border: string; text: string; edge: string }
> = {
  prosecutor:   { bg: "#B53A2E", border: "#7A1F18", text: "#FFF8F0", edge: "#B53A2E" },
  defender:     { bg: "#2C5470", border: "#16334A", text: "#F0F4F7", edge: "#2C5470" },
  investigator: { bg: "#A89F8E", border: "#5C564F", text: "#FFF8F0", edge: "#A89F8E" },
  clerk:        { bg: "#A89F8E", border: "#5C564F", text: "#FFF8F0", edge: "#A89F8E" },
  judge:        { bg: "#7A5C3F", border: "#3F2E1E", text: "#FFF8F0", edge: "#7A5C3F" },
};

const CENTER_X = 220;
const CENTER_Y = 150;
const RADIUS = 130;
const NODE_SIZE = 70;
const OPTION_NODE_WIDTH = 100;
const OPTION_NODE_HEIGHT = 40;
const OPTION_GAP = 20;

export function ArgumentMap({
  optionA,
  optionB,
  agents,
}: ArgumentMapProps) {
  const { nodes, edges } = useMemo(() => {
    const ns: Node[] = [];
    const es: Edge[] = [];

    // 截断选项名称
    const truncate = (text: string, max = 5) =>
      text.length <= max ? text : text.slice(0, max) + "…";

    // 选项 A/B 节点位置
    const d = (OPTION_NODE_WIDTH + OPTION_GAP) / 2;
    const optAX = CENTER_X - d - OPTION_NODE_WIDTH / 2;
    const optBX = CENTER_X + d - OPTION_NODE_WIDTH / 2;
    const optY = CENTER_Y - OPTION_NODE_HEIGHT / 2;

    ns.push({
      id: "option-a",
      data: { label: truncate(optionA || "选项 A") },
      position: { x: optAX, y: optY },
      style: {
        background: "#FAF8F4",
        color: "#7A1F18",
        border: "1.5px solid #B53A2E",
        borderRadius: "2px",
        padding: "6px 10px",
        fontWeight: 600,
        fontSize: "12px",
        width: `${OPTION_NODE_WIDTH}px`,
        height: `${OPTION_NODE_HEIGHT}px`,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        textAlign: "center",
        lineHeight: 1.2,
        boxSizing: "border-box",
        whiteSpace: "nowrap",
        overflow: "hidden",
        textOverflow: "ellipsis",
        fontFamily:
          '"Noto Serif SC", "ZCOOL XiaoWei", Georgia, serif',
      },
    });

    ns.push({
      id: "option-b",
      data: { label: truncate(optionB || "选项 B") },
      position: { x: optBX, y: optY },
      style: {
        background: "#FAF8F4",
        color: "#16334A",
        border: "1.5px solid #2C5470",
        borderRadius: "2px",
        padding: "6px 10px",
        fontWeight: 600,
        fontSize: "12px",
        width: `${OPTION_NODE_WIDTH}px`,
        height: `${OPTION_NODE_HEIGHT}px`,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        textAlign: "center",
        lineHeight: 1.2,
        boxSizing: "border-box",
        whiteSpace: "nowrap",
        overflow: "hidden",
        textOverflow: "ellipsis",
        fontFamily:
          '"Noto Serif SC", "ZCOOL XiaoWei", Georgia, serif',
      },
    });

    // Agent 节点：只显示有立场的角色（控方、辩方、法官）
    const STANCE_AGENTS = ["judge", "prosecutor", "defender"];
    const visibleAgents = agents
      .filter((a) => STANCE_AGENTS.includes(a.agent_type))
      .sort(
        (a, b) =>
          STANCE_AGENTS.indexOf(a.agent_type) -
          STANCE_AGENTS.indexOf(b.agent_type)
      );

    const count = visibleAgents.length;
    if (count > 0) {
      const startAngle = -Math.PI / 2;
      const angleStep = (2 * Math.PI) / count;

      visibleAgents.forEach((agent, i) => {
        const angle = startAngle + i * angleStep;
        const x = CENTER_X + RADIUS * Math.cos(angle) - NODE_SIZE / 2;
        const y = CENTER_Y + RADIUS * Math.sin(angle) - NODE_SIZE / 2;

        const colorCfg = agentColors[agent.agent_type] ?? {
          bg: "#A89F8E",
          border: "#5C564F",
          text: "#FFF8F0",
          edge: "#A89F8E",
        };

        // 名称：控方/辩方显示「选项A代表」「选项B代表」
        let displayName: string;
        if (agent.agent_type === "prosecutor") {
          displayName = `${truncate(optionA || "选项A", 4)}代表`;
        } else if (agent.agent_type === "defender") {
          displayName = `${truncate(optionB || "选项B", 4)}代表`;
        } else {
          displayName = "法官";
        }

        ns.push({
          id: `agent-${agent.agent_type}`,
          data: { label: displayName },
          position: { x, y },
          style: {
            background: colorCfg.bg,
            color: colorCfg.text,
            border: `2px solid ${colorCfg.border}`,
            borderRadius: "50%",
            width: `${NODE_SIZE}px`,
            height: `${NODE_SIZE}px`,
            padding: "3px",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            textAlign: "center",
            fontWeight: 500,
            fontSize: "10px",
            lineHeight: 1.1,
            wordBreak: "break-all",
            boxSizing: "border-box",
            boxShadow: "0 2px 4px rgba(26,24,21,0.12)",
            fontFamily:
              '"Noto Serif SC", "ZCOOL XiaoWei", Georgia, serif',
          },
          sourcePosition: Position.Bottom,
          targetPosition: Position.Top,
        });

        // 连线规则
        const isJudge = agent.agent_type === "judge";
        const biasThreshold = isJudge ? 0.52 : 0.55;
        if (agent.belief_a > biasThreshold) {
          es.push({
            id: `edge-${agent.agent_type}-a-${Math.round(agent.belief_a * 100)}`,
            source: `agent-${agent.agent_type}`,
            target: "option-a",
            type: "straight",
            style: {
              stroke: "#B53A2E",
              strokeWidth: Math.max(1, (agent.belief_a - 0.5) * 5),
              opacity: 0.85,
            },
          });
        }
        if (agent.belief_b > biasThreshold) {
          es.push({
            id: `edge-${agent.agent_type}-b-${Math.round(agent.belief_b * 100)}`,
            source: `agent-${agent.agent_type}`,
            target: "option-b",
            type: "straight",
            style: {
              stroke: "#2C5470",
              strokeWidth: Math.max(1, (agent.belief_b - 0.5) * 5),
              opacity: 0.85,
            },
          });
        }
      });
    }

    return { nodes: ns, edges: es };
  }, [optionA, optionB, agents]);

  // 容器尺寸检测（仅诊断：尺寸异常时打印）
  const containerRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!containerRef.current) return;
    const rect = containerRef.current.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) {
      console.warn(
        "[ArgumentMap] container has zero size:",
        { w: rect.width, h: rect.height },
        "— check parent flex layout"
      );
    }
  }, [agents]);

  return (
    <div
      className="w-full bg-paper border border-rule rounded-sm shadow-paper relative"
      style={{ height: 320, minHeight: 320, flexShrink: 0 }}
    >
      {/* 案卷标题（角标） */}
      <div className="absolute top-2 left-3 z-10 phase-ribbon pointer-events-none">
        意见地图
      </div>
      {/* ReactFlow 必须有明确 width/height 的父容器 — 内联 px 值 + flexShrink 防止压缩 */}
      <div
        ref={containerRef}
        style={{ width: "100%", height: 320, minHeight: 320, flexShrink: 0 }}
        className="reactflow-host"
      >
        <ReactFlowProvider>
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={EMPTY_NODE_TYPES}
            edgeTypes={EMPTY_EDGE_TYPES}
            minZoom={0.1}
            maxZoom={4}
            nodesDraggable={false}
            nodesConnectable={false}
            elementsSelectable={false}
            zoomOnScroll={true}
            panOnDrag={true}
            panOnScroll={false}
            fitView
            fitViewOptions={{ padding: 0.2 }}
          />
        </ReactFlowProvider>
      </div>
    </div>
  );
}
