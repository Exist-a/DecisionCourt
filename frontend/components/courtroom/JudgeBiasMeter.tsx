"use client";

// v2.1 O-5: 法官偏倚仪表加动效
//
// 设计 (V2.1-OPTIMIZATION-PLAN.md §Phase O-5):
//   - 指针位置已有 CSS transition (smooth move) - 保留
//   - 数字 % tween 平滑过渡 (requestAnimationFrame, 0.5s ease-out) - 新
//   - 冲击波: belief_a 显著变化 (>5%) 时, 触发一次性 dot-judge-shock keyframes
//     (复用 globals.css 已有的 v2.1 DotAvatar 冲击波样式, 但不改 avatar 本身)
//   - 指针方向色变: 偏向 A (position>50) 时指针色调暖 (prosecution), 偏向 B 时冷 (defense)
//
// 不破坏:
//   - 公开 props 契约 (beliefA / beliefB / optionA / optionB) 不变
//   - 案卷·印章 视觉风格保留 (绛红/深青 + 黑细线)
//   - 元素测试钩子 (data-testid) 保留 + 新增

import { useEffect, useRef, useState } from "react";

interface JudgeBiasMeterProps {
  beliefA: number;
  beliefB: number;
  optionA: string;
  optionB: string;
}

/**
 * 案卷·印章 风格：法官偏倚仪表
 * - 不用蓝/红渐变，用绛红/深青 + 黑色细线
 * - 中间是 serif 文字「判」
 * - 直角分隔（案卷感）
 */
export function JudgeBiasMeter({
  beliefA,
  beliefB,
  optionA,
  optionB,
}: JudgeBiasMeterProps) {
  const total = beliefA + beliefB;
  const normalizedA = total > 0 ? beliefA / total : 0.5;
  const targetPosition = Math.round(normalizedA * 100);
  const targetBeliefAPct = Math.round(beliefA * 100);
  const targetBeliefBPct = Math.round(beliefB * 100);

  // v2.1 O-5: 数字 tween — requestAnimationFrame 在 0.5s 内 ease-out 平滑到目标值
  const [displayedAPct, setDisplayedAPct] = useState(targetBeliefAPct);
  const [displayedBPct, setDisplayedBPct] = useState(targetBeliefBPct);
  const prevPositionRef = useRef(targetPosition);
  // v2.1 O-5: 冲击波触发 (key: shockKey 变化 → CSS keyframe 重新播放)
  const [shockKey, setShockKey] = useState(0);
  // v2.1 O-5: 防止初次挂载触发冲击波
  const mountedRef = useRef(false);

  useEffect(() => {
    const startAPct = displayedAPct;
    const startBPct = displayedBPct;
    const startTime = performance.now();
    const duration = 500; // 0.5s ease-out
    let raf = 0;
    const easeOut = (t: number) => 1 - Math.pow(1 - t, 3);

    function tick(now: number) {
      const elapsed = now - startTime;
      const progress = Math.min(elapsed / duration, 1);
      const eased = easeOut(progress);
      setDisplayedAPct(Math.round(startAPct + (targetBeliefAPct - startAPct) * eased));
      setDisplayedBPct(Math.round(startBPct + (targetBeliefBPct - startBPct) * eased));
      if (progress < 1) raf = requestAnimationFrame(tick);
    }
    raf = requestAnimationFrame(tick);

    // v2.1 O-5: 检测指针位置显著变化 → 触发冲击波
    const delta = Math.abs(targetPosition - prevPositionRef.current);
    if (mountedRef.current && delta > 5) {
      setShockKey((k) => k + 1);
    }
    prevPositionRef.current = targetPosition;
    mountedRef.current = true;

    return () => cancelAnimationFrame(raf);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [targetBeliefAPct, targetBeliefBPct, targetPosition]);

  const truncateOption = (text: string, maxLen = 4) => {
    if (text.length <= maxLen) return text;
    return text.slice(0, maxLen) + "…";
  };

  // v2.1 O-5: 指针颜色 — position > 50 偏 A, 偏暖 (prosecution); 否则偏冷 (defense)
  const pointerColor = targetPosition > 50 ? "#B53A2E" : "#2C5470";

  return (
    <div className="flex flex-col items-center gap-1.5 w-44 mt-1">
      <div className="flex items-baseline justify-between w-full">
        <span
          className="text-[11px] font-semibold truncate max-w-[70px] text-display"
          style={{ color: "#16334A" }}
          title={optionB}
        >
          {truncateOption(optionB)}
        </span>
        <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
          法官·判
        </span>
        <span
          className="text-[11px] font-semibold truncate max-w-[70px] text-display text-right"
          style={{ color: "#7A1F18" }}
          title={optionA}
        >
          {truncateOption(optionA)}
        </span>
      </div>

      {/* 天平横线：左深青、中黑、右绛红 */}
      <div
        className="relative w-full h-1 bg-paperDeep"
        data-testid="judge-bias-track"
      >
        <div
          className="absolute top-0 bottom-0 left-0"
          style={{
            width: "50%",
            background:
              "linear-gradient(to right, #2C5470 0%, rgba(44,84,112,0.3) 100%)",
          }}
        />
        <div
          className="absolute top-0 bottom-0 right-0"
          style={{
            width: "50%",
            background:
              "linear-gradient(to left, #B53A2E 0%, rgba(181,58,46,0.3) 100%)",
          }}
        />
        {/* 中央指针（实心方块, v2.1 O-5 加颜色随方向变化） */}
        <div
          key={`pointer-${targetPosition}`}
          className="absolute top-1/2 -translate-y-1/2 w-1.5 h-3.5 transition-all duration-500"
          style={{
            left: `calc(${targetPosition}% - 3px)`,
            backgroundColor: pointerColor,
            boxShadow: `0 0 0 2px ${pointerColor}33`,
          }}
          data-testid="judge-bias-pointer"
        />
        {/* 中点刻度 */}
        <div className="absolute top-0 bottom-0 left-1/2 -translate-x-1/2 w-px bg-ink/40" />
        {/* v2.1 O-5: 冲击波 — belief 显著变化时一次播放
            跟随指针当前位置 (targetPosition %), 而不是 track 中央,
            避免视觉错位(指针 60% 但冲击波从 50% 喷出)。 */}
        {shockKey > 0 && (
          <div
            key={`shock-${shockKey}`}
            className="dot-judge-shock-pointer dot-judge-shock-pointer-active"
            style={{
              top: "50%",
              left: `${targetPosition}%`,
              transform: "translate(-50%, -50%)",
              ["--dot-judge-gold" as string]: "#D4A24C",
            }}
            aria-hidden
            data-testid="judge-bias-shock"
          />
        )}
      </div>

      <div className="flex justify-between w-full text-[10px] font-data">
        <span
          style={{ color: targetPosition <= 50 ? "#2C5470" : "#16334A" }}
          data-testid="judge-bias-percent-b"
        >
          {displayedBPct}%
        </span>
        <span className="text-inkFaint">·</span>
        <span
          style={{ color: targetPosition > 50 ? "#B53A2E" : "#7A1F18" }}
          data-testid="judge-bias-percent-a"
        >
          {displayedAPct}%
        </span>
      </div>
    </div>
  );
}