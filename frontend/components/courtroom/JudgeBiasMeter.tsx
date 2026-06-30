"use client";

interface JudgeBiasMeterProps {
  beliefA: number;
  beliefB: number;
  optionA: string;
  optionB: string;
}

/**
 * 案卷·印章 风格：法官偏倚仪表
 * - 不用蓝/红渐变，用绛红/深青 + 黑色细线
 * - 中间是 serif 文字"判"
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
  const position = Math.round(normalizedA * 100);

  const truncateOption = (text: string, maxLen = 4) => {
    if (text.length <= maxLen) return text;
    return text.slice(0, maxLen) + "…";
  };

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
      <div className="relative w-full h-1 bg-paperDeep">
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
        {/* 中央指针（实心方块） */}
        <div
          className="absolute top-1/2 -translate-y-1/2 w-1.5 h-3.5 bg-ink transition-all duration-500"
          style={{ left: `calc(${position}% - 3px)` }}
        />
        {/* 中点刻度 */}
        <div className="absolute top-0 bottom-0 left-1/2 -translate-x-1/2 w-px bg-ink/40" />
      </div>

      <div className="flex justify-between w-full text-[10px] font-data">
        <span style={{ color: "#2C5470" }}>{Math.round(beliefB * 100)}%</span>
        <span className="text-inkFaint">·</span>
        <span style={{ color: "#B53A2E" }}>{Math.round(beliefA * 100)}%</span>
      </div>
    </div>
  );
}
