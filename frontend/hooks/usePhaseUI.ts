"use client";

import { useMemo } from "react";
import type { CourtPhase } from "@/types";

/**
 * 单一数据源：所有阶段相关的 UI 文案。
 *
 * 任何"当前阶段显示什么文字"都从这里读，避免散落在多个组件里出现
 * "deliberation 阶段还显示 · 第 0 轮"、"判决书生成时还显示等待下一位 Agent"
 * 之类的不一致问题。
 */
export interface PhaseUI {
  /** 顶部小标题 + MessageHistory 标签用的简短名称 */
  phaseLabel: string;
  /** 圆点进度条 + 副标题右侧的轮次标识（如「第 2 轮」），不适用时为 null */
  roundLabel: string | null;
  /** PhaseGuide 横幅的主文案 */
  guideText: string;
  /** 横幅底色（slate / amber / emerald） */
  guideTone: "slate" | "amber" | "emerald";
  /** AgentArena 右上角的小气泡提示 */
  speakerHint: string;
  /** 输入框 placeholder */
  placeholder: string;
  /** 是否禁用输入框 */
  inputDisabled: boolean;
  /** 输入框下方帮助文字，可选 */
  inputHelpText: string | null;
  /** 是否允许显示 PhaseGuide */
  showGuide: boolean;
}

const PHASE_LABELS: Record<CourtPhase, string> = {
  idle: "立案",
  clarification: "问题澄清",
  option_generation: "选项生成",
  opening: "开庭陈述",
  evidence: "举证阶段",
  cross_exam: "质证阶段",
  closing: "结案陈词",
  deliberation: "判决生成中",
  verdict: "判决已生成",
  appeal: "上诉/再审",
};

// 只有质证阶段才有"轮次"概念
const ROUNDED_PHASES: CourtPhase[] = ["cross_exam"];

interface UsePhaseUIParams {
  phase: CourtPhase;
  round?: number;
  /** 判决书是否已经生成（即便 phase 还没切到 verdict） */
  verdictReady?: boolean;
  /** 是否正在等待用户点击"开始下一轮" */
  waitingForNextRound?: boolean;
  /** 等待用户点击时，下一轮的轮次 */
  nextRound?: number;
  /** 当前是否有人正在发言（用于 speakerHint 优先级最高） */
  isAnyAgentSpeaking?: boolean;
  /** 当前发言 Agent 的中文名（如「辩方」），用于 speakerHint */
  currentSpeakerName?: string | null;
}

export function usePhaseUI(params: UsePhaseUIParams): PhaseUI {
  const {
    phase,
    round = 0,
    verdictReady = false,
    waitingForNextRound = false,
    nextRound = 2,
    isAnyAgentSpeaking = false,
    currentSpeakerName = null,
  } = params;

  return useMemo<PhaseUI>(() => {
    const phaseLabel = PHASE_LABELS[phase] ?? phase;

    // 轮次标签：只有质证阶段才显示，避免 deliberation 显示「第 0 轮」
    const roundLabel =
      ROUNDED_PHASES.includes(phase) && round > 0 ? `第 ${round} 轮` : null;

    // 默认值（按 phase 填充，下面再覆盖特例）
    let guideTone: PhaseUI["guideTone"] = "slate";
    let guideText = "庭审进行中，请稍候。";
    let speakerHint = "等待下一位 Agent 发言…";
    let placeholder = "输入证据内容或直接发送…";
    let inputDisabled = false;
    let inputHelpText: string | null = null;
    let showGuide = phase !== "idle";

    // ===== 优先级最高的特例：判决已生成 =====
    if (verdictReady || phase === "verdict") {
      guideTone = "emerald";
      guideText = "判决书已生成，请查看最终结论与建议。";
      speakerHint = "判决书已就绪，可点击下方按钮查看";
      placeholder = "庭审已结束";
      inputDisabled = true;
    }
    // ===== 等待用户开始下一轮 =====
    else if (waitingForNextRound) {
      guideTone = "amber";
      if (phase === "opening") {
        guideText = "开庭陈述结束，点击下方「开始质证」进入质证阶段，你可先补充证据。";
      } else {
        guideText = `第 ${nextRound} 轮质证等待开始，点击下方「开始第 ${nextRound} 轮」继续，你可先补充证据。`;
      }
      speakerHint = `等待你开始第 ${nextRound} 轮`;
      placeholder = `准备进入第 ${nextRound} 轮质证，可先补充证据…`;
    }
    // ===== 按阶段填充 =====
    else {
      switch (phase) {
        case "idle":
          showGuide = false;
          speakerHint = "点击右上角「开始庭审」启动辩论";
          placeholder = "庭审尚未开始";
          inputDisabled = true;
          break;

        case "clarification":
          guideText = "调查员正在澄清问题，请回答以完善案情。";
          speakerHint = "等待你回答调查员的提问";
          placeholder = "请回答调查员的问题…";
          break;

        case "option_generation":
          guideText = "系统正在生成候选选项，请从中选择两个进入庭审。";
          speakerHint = "等待你确认选项";
          placeholder = "请选择两个候选选项";
          inputDisabled = true;
          break;

        case "opening":
          guideText = "控辩双方正在做开场陈述，你可以准备要提交的证据。";
          speakerHint = "庭审开场中";
          placeholder = "输入证据内容或直接发送…";
          break;

        case "evidence":
          guideText = "举证阶段：点击下方「+ 提交证据」补充事实、数据或约束。";
          speakerHint = "等待你提交证据";
          placeholder = "输入要提交的证据内容…";
          break;

        case "cross_exam":
          guideText = `双方正在质证攻防${roundLabel ? `（${roundLabel}）` : ""}。你可随时提交新证据。`;
          speakerHint = "质证进行中";
          placeholder = "输入新证据内容或直接发送…";
          break;

        case "closing":
          guideText = "结案陈词：双方总结观点，仍可最后补充证据。";
          speakerHint = "结案陈词中";
          placeholder = "结案陈词阶段，不再接受新证据…";
          inputDisabled = true;
          inputHelpText = "如需补充证据，可点击「+ 提交证据」按钮";
          break;

        case "deliberation":
          guideTone = "amber";
          guideText = "书记员正在整理庭审记录，生成判决书，请稍候。";
          speakerHint = "判决书生成中…";
          placeholder = "判决书生成中…";
          inputDisabled = true;
          break;

        case "appeal":
          guideText = "如需补充证据重新辩论，可点击「直接判决」旁的选项。";
          speakerHint = "等待你的下一步操作";
          placeholder = "可补充证据重新辩论";
          break;
      }
    }

    // ===== Speaker hint 最高优先级 =====
    if (isAnyAgentSpeaking && currentSpeakerName) {
      speakerHint = `${currentSpeakerName} 正在发言`;
    }

    return {
      phaseLabel,
      roundLabel,
      guideText,
      guideTone,
      speakerHint,
      placeholder,
      inputDisabled,
      inputHelpText,
      showGuide,
    };
  }, [
    phase,
    round,
    verdictReady,
    waitingForNextRound,
    nextRound,
    isAnyAgentSpeaking,
    currentSpeakerName,
  ]);
}
