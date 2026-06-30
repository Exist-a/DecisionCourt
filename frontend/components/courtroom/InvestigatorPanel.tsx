"use client";

import { useEffect, useState } from "react";
import { useCourtroomStore } from "@/store/courtroomStore";
import type { InvestigationEvent } from "@/store/courtroomStore";

/**
 * InvestigatorPanel 是庭审页右侧（或顶部折叠 Tab）的「调查活动」面板：
 *   - 列出每次调查员派遣（dispatch）和回报（report）
 *   - 派遣 → 报告中间出现 loading spinner 表示「调查中」
 *   - 与 CoT 折叠面板的简略摘要不同，这里展示完整 query / summary /
 *     finding_id
 *
 * 数据源是 store.investigationEvents，由 agent.cot_step（dispatch）和
 * a2a.message（report）事件合并得到。
 */
export function InvestigatorPanel() {
  const events = useCourtroomStore((s) => s.investigationEvents);

  return (
    <div className="bg-paper border border-rule rounded-sm shadow-paper h-full flex flex-col">
      <div className="px-3 py-2 border-b border-rule flex items-baseline justify-between">
        <h3 className="text-display text-sm font-semibold text-ink flex items-center gap-1.5">
          <span aria-hidden>🔍</span>
          调查活动
        </h3>
        <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
          {events.length}
        </span>
      </div>

      <div className="flex-1 overflow-y-auto px-3 py-2 space-y-1.5">
        {events.length === 0 ? (
          <div className="text-center py-6 text-[11px] text-inkFaint text-display">
            尚无调查活动
          </div>
        ) : (
          events.map((ev) => <EventRow key={ev.id} event={ev} />)
        )}
      </div>
    </div>
  );
}

interface EventRowProps {
  event: InvestigationEvent;
}

function EventRow({ event }: EventRowProps) {
  const isSearching = event.status === "searching";
  const isFailed = event.status === "failed";
  const isDone = event.status === "completed" || isFailed;
  const hasDetails = isDone && (event.rawResults?.length ?? 0) > 0;
  const [age, setAge] = useState(0);
  // 行可点击展开：让用户能看到搜索结果原文（之前只有摘要）。
  const [expanded, setExpanded] = useState(false);

  useEffect(() => {
    const t = setInterval(() => {
      const ms = Date.now() - Date.parse(event.createdAt);
      setAge(Number.isNaN(ms) ? 0 : Math.floor(ms / 1000));
    }, 1000);
    return () => clearInterval(t);
  }, [event.createdAt]);

  const dispatcherLabel = event.dispatcher === "prosecutor" ? "控方" : "辩方";

  return (
    <div
      className={`text-[11px] leading-snug rounded-sm border ${
        isSearching
          ? "bg-paperDeep border-inkSoft"
          : isFailed
            ? "bg-paper border-seal/40"
            : "bg-white border-rule shadow-paper"
      }`}
      data-event-status={event.status ?? "unknown"}
    >
      <button
        type="button"
        onClick={() => hasDetails && setExpanded((v) => !v)}
        disabled={!hasDetails}
        className={`w-full text-left px-2 py-1.5 ${
          hasDetails ? "cursor-pointer hover:bg-paperDeep" : "cursor-default"
        }`}
        aria-expanded={expanded}
        data-row-clickable={hasDetails ? "true" : "false"}
      >
        <div className="flex items-baseline gap-1.5">
          {isSearching ? (
            <span className="text-inkSoft">→</span>
          ) : (
            <span className="text-prosecution-ink">←</span>
          )}
          <span className="font-data text-[10px] text-ink">{dispatcherLabel}</span>
          <span className="text-inkSoft">
            {isSearching ? "调查员搜索中" : isFailed ? "调查失败" : "调查员回报"}
          </span>
          {isSearching ? (
            <Spinner />
          ) : isFailed ? (
            <span aria-hidden className="text-seal text-[12px]">!</span>
          ) : (
            <span aria-hidden className="text-[12px] text-inkSoft">✓</span>
          )}
          {hasDetails && (
            <span className="ml-auto text-inkFaint text-[10px]">
              {expanded ? "▾" : "▸"}
            </span>
          )}
        </div>
        <div className="mt-0.5 text-display text-[11px] text-ink">
          「{truncate(event.query, 60)}」
        </div>
        {isDone && event.summary && (
          <div className="mt-0.5 text-[10px] text-inkSoft text-display">
            {event.summary}
          </div>
        )}
        {isDone && event.findingId && (
          <div className="mt-0.5 text-[10px] text-inkFaint font-data">
            finding_id={event.findingId.slice(0, 8)}…
            {event.resultCount !== undefined && ` · ${event.resultCount} 条结果`}
          </div>
        )}
        {isFailed && (
          <div className="mt-0.5 text-[10px] text-seal-ink text-display">
            搜索失败，请稍后再试
          </div>
        )}
        <div className="mt-0.5 text-[9px] text-inkFaint font-data">
          {age}s 前
          {hasDetails && !expanded && (
            <span className="ml-2 text-prosecution-ink underline-offset-2 group-hover:underline">
              点击查看 {event.resultCount} 条搜索结果
            </span>
          )}
        </div>
      </button>

      {expanded && hasDetails && (
        <ol
          className="border-t border-rule bg-paperDeep/60 px-2 py-1.5 space-y-1.5"
          data-row-expanded="true"
        >
          {event.rawResults!.map((raw, idx) => {
            // raw 格式："title | url | content" （来自 resultsToStrings）
            const parts = raw.split(" | ", 3);
            const [title = "", url = "", content = ""] = parts;
            return (
              <li key={idx} className="text-[10px] leading-snug">
                <div className="flex items-baseline gap-1.5">
                  <span className="font-data text-inkFaint shrink-0">
                    [{idx + 1}]
                  </span>
                  <span className="text-display text-ink font-semibold">
                    {title || url || "(无标题)"}
                  </span>
                </div>
                {url && (
                  <a
                    href={url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="ml-3 text-[10px] text-prosecution-ink hover:underline break-all font-data"
                  >
                    {url}
                  </a>
                )}
                {content && (
                  <p className="ml-3 mt-0.5 text-display text-inkSoft">
                    {content}
                  </p>
                )}
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}

function Spinner() {
  return (
    <span
      className="inline-block w-2.5 h-2.5 border border-inkSoft border-t-transparent rounded-full animate-spin ml-1"
      aria-label="调查员正在搜索"
    />
  );
}

function truncate(s: string, max: number) {
  return s.length > max ? `${s.slice(0, max)}…` : s;
}