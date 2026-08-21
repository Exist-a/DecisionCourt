"use client";

// v2.0 厕所标识剪影小人 — Silhouette 组件
//
// 设计原则 (V2.0-PLAN.md §1.2):
//   - 风格: 纯色剪影 (无渐变/无细节/左右对称)
//   - 5 角色: 控方律师/辩方律师/调查员/法官/书记员
//   - viewBox: 64x64 (紧凑可缩放)
//   - 颜色: CSS var 解耦 (currentColor + silhouette-* class)
//
// 5 角色特征 (plan §1.2):
//   - 控方律师: 圆形头 + 站立 + 举手指控 (指尖向上)
//   - 辩方律师: 圆形头 + 站立 + 双手打开辩护
//   - 调查员: 圆形头 + 蹲下 + 拿放大镜
//   - 法官: 圆形头 + 坐姿 + 拿法槌
//   - 书记员: 圆形头 + 低头打字

import type { AgentType } from "@/types";

interface SilhouetteProps {
  agentType: AgentType;
  isSpeaking?: boolean;
  isThinking?: boolean;
  isSearching?: boolean;
  isJudging?: boolean;
  size?: number;
}

/**
 * 入口组件 — 根据 agentType 选对应 SVG 组件。
 * 5 角色 SVG 拆成独立子组件 (ProsecutorSilhouette / DefenderSilhouette 等),
 * 便于未来单独调整某个角色的姿势而互不影响。
 */
export function Silhouette({
  agentType,
  isSpeaking,
  isThinking,
  isSearching,
  isJudging,
  size = 64,
}: SilhouetteProps) {
  const className = `silhouette-${agentType} silhouette-avatar`;

  switch (agentType) {
    case "prosecutor":
      return <ProsecutorSilhouette className={className} size={size} isSpeaking={isSpeaking} />;
    case "defender":
      return <DefenderSilhouette className={className} size={size} isSpeaking={isSpeaking} />;
    case "investigator":
      return (
        <InvestigatorSilhouette className={className} size={size} isSearching={isSearching} />
      );
    case "judge":
      return <JudgeSilhouette className={className} size={size} isJudging={isJudging} />;
    case "clerk":
      return <ClerkSilhouette className={className} size={size} isThinking={isThinking} />;
    default:
      return <DefenderSilhouette className={className} size={size} />;
  }
}

// --- 5 角色 SVG (控方/辩方/调查员/法官/书记员) ---
// 设计原则:
//   - viewBox 64x64 (紧凑)
//   - currentColor + tailwind/CSS var 控色
//   - 走路动画用 silhouette-leg-left / silhouette-leg-right (CSS keyframes)
//   - 点头动画用 silhouette-nod class
//   - 律师举手指控用 silhouette-arm-pointing
//   - 法官敲锤用 silhouette-gavel
//   - 调查员放大镜用 silhouette-magnifier

function ProsecutorSilhouette({
  className,
  size,
  isSpeaking,
}: {
  className: string;
  size: number;
  isSpeaking?: boolean;
}) {
  return (
    <svg
      viewBox="0 0 64 64"
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      className={className}
      data-role="prosecutor"
      data-speaking={isSpeaking ? "true" : "false"}
    >
      {/* 头 */}
      <circle cx="32" cy="14" r="8" fill="currentColor" />
      {/* 身体 */}
      <rect x="22" y="22" width="20" height="22" rx="2" fill="currentColor" />
      {/* 左脚 (走路动画分离) */}
      <rect
        x="24"
        y="44"
        width="6"
        height="16"
        rx="1"
        className={isSpeaking ? "silhouette-leg-left" : ""}
        fill="currentColor"
      />
      {/* 右脚 */}
      <rect
        x="34"
        y="44"
        width="6"
        height="16"
        rx="1"
        className={isSpeaking ? "silhouette-leg-right" : ""}
        fill="currentColor"
      />
      {/* 举手指控 (左侧手臂) */}
      <rect
        x="14"
        y="20"
        width="3"
        height="14"
        rx="1"
        className={isSpeaking ? "silhouette-arm-pointing" : ""}
        fill="currentColor"
      />
    </svg>
  );
}

function DefenderSilhouette({
  className,
  size,
  isSpeaking,
}: {
  className: string;
  size: number;
  isSpeaking?: boolean;
}) {
  return (
    <svg
      viewBox="0 0 64 64"
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      className={className}
      data-role="defender"
      data-speaking={isSpeaking ? "true" : "false"}
    >
      <circle cx="32" cy="14" r="8" fill="currentColor" />
      <rect x="22" y="22" width="20" height="22" rx="2" fill="currentColor" />
      <rect
        x="24"
        y="44"
        width="6"
        height="16"
        rx="1"
        className={isSpeaking ? "silhouette-leg-left" : ""}
        fill="currentColor"
      />
      <rect
        x="34"
        y="44"
        width="6"
        height="16"
        rx="1"
        className={isSpeaking ? "silhouette-leg-right" : ""}
        fill="currentColor"
      />
      {/* 双手打开辩护 (左右对称) */}
      <rect x="10" y="22" width="3" height="14" rx="1" fill="currentColor" />
      <rect x="51" y="22" width="3" height="14" rx="1" fill="currentColor" />
    </svg>
  );
}

function InvestigatorSilhouette({
  className,
  size,
  isSearching,
}: {
  className: string;
  size: number;
  isSearching?: boolean;
}) {
  return (
    <svg
      viewBox="0 0 64 64"
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      className={className}
      data-role="investigator"
      data-searching={isSearching ? "true" : "false"}
    >
      <circle cx="32" cy="14" r="8" fill="currentColor" />
      {/* 蹲下: 身体矮 + 重心低 */}
      <rect x="22" y="22" width="20" height="14" rx="2" fill="currentColor" />
      {/* 双脚 (蹲姿, 弯曲) */}
      <rect x="22" y="36" width="8" height="14" rx="1" fill="currentColor" />
      <rect x="34" y="36" width="8" height="14" rx="1" fill="currentColor" />
      {/* 放大镜 (右侧) */}
      <circle
        cx="50"
        cy="26"
        r="6"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        className={isSearching ? "silhouette-magnifier" : ""}
      />
      <rect x="48" y="32" width="2" height="8" rx="1" fill="currentColor" />
    </svg>
  );
}

function JudgeSilhouette({
  className,
  size,
  isJudging,
}: {
  className: string;
  size: number;
  isJudging?: boolean;
}) {
  return (
    <svg
      viewBox="0 0 64 64"
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      className={className}
      data-role="judge"
      data-judging={isJudging ? "true" : "false"}
    >
      <circle cx="32" cy="14" r="8" fill="currentColor" />
      {/* 坐姿: 身体短 + 腿横向 */}
      <rect x="20" y="22" width="24" height="20" rx="2" fill="currentColor" />
      {/* 双腿横向 (坐姿) */}
      <rect x="14" y="42" width="36" height="6" rx="1" fill="currentColor" />
      {/* 法槌 (右上角) */}
      <rect
        x="46"
        y="8"
        width="4"
        height="12"
        rx="1"
        className={isJudging ? "silhouette-gavel" : ""}
        fill="currentColor"
      />
      <rect x="44" y="6" width="8" height="4" rx="1" fill="currentColor" />
    </svg>
  );
}

function ClerkSilhouette({
  className,
  size,
  isThinking,
}: {
  className: string;
  size: number;
  isThinking?: boolean;
}) {
  return (
    <svg
      viewBox="0 0 64 64"
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      className={className}
      data-role="clerk"
      data-thinking={isThinking ? "true" : "false"}
    >
      <circle cx="32" cy="14" r="8" fill="currentColor" />
      <rect x="22" y="22" width="20" height="22" rx="2" fill="currentColor" />
      <rect x="24" y="44" width="6" height="16" rx="1" fill="currentColor" />
      <rect x="34" y="44" width="6" height="16" rx="1" fill="currentColor" />
      {/* 低头打字: 笔记本 + 手 */}
      <rect x="14" y="38" width="14" height="2" rx="1" fill="currentColor" />
      <rect x="36" y="38" width="14" height="2" rx="1" fill="currentColor" />
      {/* 点头动画 */}
      <g className={isThinking ? "silhouette-nod" : ""}>
        <circle cx="32" cy="14" r="8" fill="currentColor" />
      </g>
    </svg>
  );
}
