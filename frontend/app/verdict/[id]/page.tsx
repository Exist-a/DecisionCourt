"use client";

import { useEffect, useMemo, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { api } from "@/lib/api";
import { useCourtroomStore } from "@/store/courtroomStore";
import { Button } from "@/components/ui/button";
import {
  ThumbsUp,
  ThumbsDown,
  RotateCcw,
  CheckCircle2,
  AlertCircle,
  Scale,
  Bookmark,
  MessageSquare,
  Download,
  FileText,
  Loader2,
} from "lucide-react";
import { BehindTheScenesPanel } from "@/components/courtroom/BehindTheScenesPanel";
import type { Message } from "@/types";

export default function VerdictPage() {
  const params = useParams();
  const router = useRouter();
  const sessionId = params.id as string;
  const session = useCourtroomStore((s) => s.session);
  const storeVerdict = useCourtroomStore((s) => s.verdict);
  const setVerdict = useCourtroomStore((s) => s.setVerdict);
  // v0.5: pull the v0.5 episodic-memory timeline from the live courtroom
  // store. It was hydrated during the trial via a2a.message WebSocket
  // events, so the verdict page can render the full behind-the-scenes view
  // without a separate REST fetch.
  const memoryEntries = useCourtroomStore((s) => s.memoryEntries);
  const verdict = storeVerdict;
  const [loading, setLoading] = useState(!storeVerdict);
  const [feedback, setFeedback] = useState<"helpful" | "not_helpful" | null>(
    null,
  );
  // 印章落印动效 — 仅首次进入时播放一次
  const [sealLanded, setSealLanded] = useState(false);
  // 庭审对话记录
  const [messages, setMessages] = useState<Message[]>([]);
  // v0.5+：导出按钮 loading / 错误态
  const [exportingJSON, setExportingJSON] = useState(false);
  const [exportError, setExportError] = useState<string | null>(null);

  useEffect(() => {
    if (storeVerdict) return;

    async function load() {
      try {
        const res = await api.getVerdict(sessionId);
        if (res.code === 0) {
          setVerdict(res.data);
        }
      } finally {
        setLoading(false);
      }
    }

    load();
  }, [sessionId, storeVerdict, setVerdict]);

  // 加载庭审对话
  useEffect(() => {
    async function loadMsgs() {
      try {
        const res = await api.getMessages(sessionId);
        if (res.code === 0) {
          setMessages(res.data.messages ?? []);
        }
      } catch {
        // ignore — verdict page works without messages
      }
    }
    loadMsgs();
  }, [sessionId]);

  // 解析共识 / 争议焦点（兼容 string(JSON)、array、空值）
  const { consensusPoints, divergencePoints, scoreA, scoreB } = useMemo(() => {
    const parsePoints = (raw: unknown): string[] => {
      if (Array.isArray(raw)) return raw.map((s) => String(s));
      if (typeof raw === "string" && raw.trim()) {
        try {
          const parsed = JSON.parse(raw);
          return Array.isArray(parsed)
            ? parsed.map((s: unknown) => String(s))
            : [];
        } catch {
          return [raw];
        }
      }
      return [];
    };
    return {
      consensusPoints: parsePoints(verdict?.consensus_points),
      divergencePoints: parsePoints(verdict?.divergence_points),
      scoreA: verdict?.option_a_score ?? 0,
      scoreB: verdict?.option_b_score ?? 0,
    };
  }, [verdict]);

  // 判决书正文段（必须在所有 early return 之前调用 — Rules of Hooks）
  const contentBlocks = useMemo(
    () => (verdict ? parseMarkdown(verdict.content) : []),
    [verdict],
  );

  // 庭审流程对话（按角色分组）
  const transcript = useMemo(() => groupTranscript(messages), [messages]);

  useEffect(() => {
    if (!verdict || sealLanded) return;
    const t = setTimeout(() => setSealLanded(true), 650);
    return () => clearTimeout(t);
  }, [verdict, sealLanded]);

  // v0.5+：导出 JSON —— 调后端 export 端点，浏览器下载。
  const handleExportJSON = async () => {
    setExportingJSON(true);
    setExportError(null);
    try {
      await api.exportSession(sessionId);
    } catch (e) {
      setExportError(e instanceof Error ? e.message : "导出失败");
    } finally {
      setExportingJSON(false);
    }
  };

  // v0.5+：导出 PDF —— 客户端 window.print() 配合 globals.css 里的 print 样式。
  const handleExportPDF = () => {
    api.printVerdictAsPDF();
  };

  if (loading) {
    return (
      <div className="min-h-screen bg-paper text-ink flex items-center justify-center paper-overlay">
        <div className="text-center">
          <span className="seal-stamp w-16 h-16 text-2xl inline-flex items-center justify-center animate-pulse">
            判
          </span>
          <p className="text-display text-base text-inkSoft mt-4">
            书记员正在整理判决书…
          </p>
          <p className="text-[10px] uppercase tracking-[0.25em] text-inkFaint font-data mt-2">
            Generating Verdict
          </p>
        </div>
      </div>
    );
  }

  if (!verdict) {
    return (
      <div className="min-h-screen bg-paper text-ink flex items-center justify-center">
        <div className="text-center">
          <p className="text-display text-base text-inkSoft">未找到判决书</p>
          <Button
            onClick={() => router.push("/")}
            className="mt-4 bg-ink text-paper hover:bg-inkSoft rounded-sm"
          >
            返 回 立 案
          </Button>
        </div>
      </div>
    );
  }

  const recommended: "A" | "B" = scoreA >= scoreB ? "A" : "B";
  const recommendedName = recommended === "A"
    ? (session?.option_a ?? "选项 A")
    : (session?.option_b ?? "选项 B");
  const optionA = session?.option_a ?? "选项 A";
  const optionB = session?.option_b ?? "选项 B";
  const caseTitle = session?.title ?? "决策案";

  return (
    <div className="min-h-screen bg-paper text-ink paper-overlay">
      {/* ============= 顶部案卷封面 ============= */}
      <header className="border-b border-rule bg-paperDeep">
        <div className="container mx-auto max-w-4xl px-6 py-4 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <span className="seal-stamp w-9 h-9 text-[15px] flex items-center justify-center">
              判
            </span>
            <div>
              <h2 className="text-display text-base font-semibold text-ink leading-tight">
                决 策 庭 · 判 决 書
              </h2>
              <p className="text-[10px] uppercase tracking-[0.25em] text-inkFaint font-data leading-tight mt-0.5">
                DecisionCourt Verdict
              </p>
            </div>
          </div>
          <div className="hidden md:flex items-center gap-3">
            <div className="flex items-center gap-2 text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
              <Bookmark className="w-3 h-3" />
              Case No. DC-{verdict.verdict_id?.slice(0, 8) ?? "—"}
            </div>
            {/* v0.5+：导出按钮 — 屏幕可见，打印时自动隐藏（@media print） */}
            <div className="flex items-center gap-1.5 print:hidden">
              <Button
                variant="outline"
                size="sm"
                onClick={handleExportJSON}
                disabled={exportingJSON}
                className="bg-white border border-rule text-ink hover:bg-paper rounded-sm h-8 px-3 text-[11px] font-data tracking-wider"
                title="导出庭审完整数据为 JSON 文件（含双方公开记录 + 你能看到的私有策略笔记）"
              >
                {exportingJSON ? (
                  <Loader2 className="w-3 h-3 mr-1.5 animate-spin" />
                ) : (
                  <Download className="w-3 h-3 mr-1.5" />
                )}
                导出 JSON
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={handleExportPDF}
                className="bg-white border border-rule text-ink hover:bg-paper rounded-sm h-8 px-3 text-[11px] font-data tracking-wider"
                title="通过浏览器打印对话框保存为 PDF"
              >
                <FileText className="w-3 h-3 mr-1.5" />
                导出 PDF
              </Button>
            </div>
          </div>
        </div>
        {exportError && (
          <div className="container mx-auto max-w-4xl px-6 pb-2 text-[11px] text-prosecution-ink font-data print:hidden">
            导出失败：{exportError}
          </div>
        )}
      </header>

      {/* ============= Hero · 印章落印 ============= */}
      <section className="container mx-auto max-w-4xl px-6 pt-12 pb-8">
        <div className="relative bg-white border border-rule shadow-paper-lg overflow-hidden">
          {/* 案件主题行（在卡片内顶部，不再溢出/遮挡） */}
          <div className="bg-paperDeep border-b border-rule px-6 py-2.5 flex items-center justify-between">
            <span className="text-[10px] uppercase tracking-[0.25em] text-inkFaint font-data shrink-0">
              案件主题
            </span>
            <span className="text-display text-sm text-ink truncate ml-3">
              {caseTitle}
            </span>
          </div>

          <div className="px-8 pt-12 pb-8 md:px-12 md:pt-14 md:pb-10 text-center">
            {/* 落印动效 */}
            <div
              className={`mx-auto mb-6 ${sealLanded ? "" : "animate-sealDrop"}`}
              aria-label="判决印章"
            >
              <span className="seal-stamp w-20 h-20 text-3xl inline-flex items-center justify-center">
                判
              </span>
            </div>

            {/* 案号 */}
            <p className="text-[10px] uppercase tracking-[0.3em] text-inkFaint font-data mb-3">
              Case No. DC-{verdict.verdict_id?.slice(0, 8).toUpperCase() ?? "—"}
              <span className="mx-2">·</span>
              {new Date(verdict.created_at ?? Date.now())
                .toISOString()
                .slice(0, 10)}
            </p>

            <h1 className="text-display text-3xl md:text-4xl font-semibold text-ink leading-tight mb-4">
              判 决 書
            </h1>

            <p className="text-display text-base text-inkSoft max-w-2xl mx-auto leading-relaxed">
              {verdict.summary || "（无摘要）"}
            </p>

            {/* 推荐选项 · 突出 */}
            <div className="mt-6 inline-flex items-center gap-2 px-5 py-2.5 bg-ink text-paper">
              <Scale className="w-4 h-4" />
              <span className="text-[10px] uppercase tracking-[0.2em] font-data opacity-70">
                采纳
              </span>
              <span className="text-display text-base font-semibold">
                {recommended === "A" ? "选项 A" : "选项 B"} · {recommendedName}
              </span>
            </div>
          </div>
        </div>
      </section>

      {/* ============= 双方得分对照 ============= */}
      <section className="container mx-auto max-w-4xl px-6 pb-6">
        <div className="grid md:grid-cols-[1fr_auto_1fr] gap-0 border border-rule rounded-sm overflow-hidden bg-white shadow-paper">
          {/* 选项 A */}
          <div className="bg-paper p-6 border-r border-prosecution/30 relative">
            <div className="absolute top-0 left-0 right-0 h-1 bg-prosecution" />
            <div className="flex items-center justify-between mb-3">
              <span className="text-display text-sm font-semibold text-prosecution-ink">
                控方主张 · 选 项 A
              </span>
              {recommended === "A" && (
                <span className="text-[9px] uppercase tracking-[0.2em] font-data px-1.5 py-0.5 bg-ink text-paper">
                  推 荐
                </span>
              )}
            </div>
            <p className="text-display text-lg font-semibold text-ink leading-tight mb-4">
              {optionA}
            </p>
            <div className="flex items-baseline gap-1.5 mb-2">
              <span className="text-display text-3xl font-semibold text-prosecution-ink font-data">
                {Math.round(scoreA * 100)}
              </span>
              <span className="text-sm text-inkFaint font-data">分</span>
              <span className="ml-auto text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
                Score
              </span>
            </div>
            <div className="h-1 bg-paperDeep relative">
              <div
                className="h-full bg-prosecution transition-all"
                style={{ width: `${Math.round(scoreA * 100)}%` }}
              />
            </div>
          </div>

          {/* 中央 · 天平 */}
          <div className="flex items-center justify-center px-3 bg-paperDeep">
            <div className="text-display text-inkFaint text-3xl font-light">
              ⚖
            </div>
          </div>

          {/* 选项 B */}
          <div className="bg-paper p-6 relative">
            <div className="absolute top-0 left-0 right-0 h-1 bg-defense" />
            <div className="flex items-center justify-between mb-3">
              <span className="text-display text-sm font-semibold text-defense-ink">
                辩方主张 · 选 项 B
              </span>
              {recommended === "B" && (
                <span className="text-[9px] uppercase tracking-[0.2em] font-data px-1.5 py-0.5 bg-ink text-paper">
                  推 荐
                </span>
              )}
            </div>
            <p className="text-display text-lg font-semibold text-ink leading-tight mb-4">
              {optionB}
            </p>
            <div className="flex items-baseline gap-1.5 mb-2">
              <span className="text-display text-3xl font-semibold text-defense-ink font-data">
                {Math.round(scoreB * 100)}
              </span>
              <span className="text-sm text-inkFaint font-data">分</span>
              <span className="ml-auto text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
                Score
              </span>
            </div>
            <div className="h-1 bg-paperDeep relative">
              <div
                className="h-full bg-defense transition-all"
                style={{ width: `${Math.round(scoreB * 100)}%` }}
              />
            </div>
          </div>
        </div>
      </section>

      {/* ============= 共识 / 争议焦点 ============= */}
      <section className="container mx-auto max-w-4xl px-6 pb-6">
        <div className="grid md:grid-cols-2 gap-5">
          {/* 共识点 */}
          <article className="bg-white border border-rule shadow-paper p-6 relative">
            <div className="absolute top-0 left-0 right-0 h-px bg-defense" />
            <div className="flex items-baseline justify-between mb-3">
              <h3 className="text-display text-base font-semibold text-defense-ink flex items-center gap-2">
                <CheckCircle2 className="w-4 h-4" />
                共 识
              </h3>
              <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
                Consensus
              </span>
            </div>
            <PointsList points={consensusPoints} tone="defense" />
          </article>

          {/* 争议焦点 */}
          <article className="bg-white border border-rule shadow-paper p-6 relative">
            <div className="absolute top-0 left-0 right-0 h-px bg-prosecution" />
            <div className="flex items-baseline justify-between mb-3">
              <h3 className="text-display text-base font-semibold text-prosecution-ink flex items-center gap-2">
                <AlertCircle className="w-4 h-4" />
                争 议
              </h3>
              <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
                Divergence
              </span>
            </div>
            <PointsList points={divergencePoints} tone="prosecution" />
          </article>
        </div>
      </section>

      {/* ============= 可执行建议 ============= */}
      {verdict.recommendation && (
        <section className="container mx-auto max-w-4xl px-6 pb-6">
          <div className="bg-ink text-paper border border-ink shadow-paper-lg p-6 md:p-8 relative">
            <div className="absolute top-0 left-0 right-0 h-px bg-gold" />
            <div className="flex items-baseline justify-between mb-3">
              <h3 className="text-display text-base font-semibold text-paper flex items-center gap-2">
                <Scale className="w-4 h-4 text-gold" />
                可 执 行 建 议
              </h3>
              <span className="text-[10px] uppercase tracking-[0.2em] text-paper/60 font-data">
                Recommendation
              </span>
            </div>
            <p className="text-display text-base leading-relaxed text-paper/95">
              {verdict.recommendation}
            </p>
          </div>
        </section>
      )}

      {/* ============= v0.5+ 庭审纪要 ============= */}
      {/* 与 verdict.summary（采纳建议）不同：这里给用户"庭审中发生了什么"
          的 1-2 句叙事，让用户能复述整场过程而不必滚 transcript。 */}
      {verdict.trial_summary && (
        <section className="container mx-auto max-w-4xl px-6 pb-6">
          <div className="bg-paperDeep border-l-2 border-judge px-6 py-5 relative">
            <div className="flex items-baseline justify-between mb-3">
              <h3 className="text-display text-base font-semibold text-ink flex items-center gap-2">
                <MessageSquare className="w-4 h-4 text-judge" />
                庭 审 纪 要
              </h3>
              <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
                Trial Summary
              </span>
            </div>
            <p className="text-display text-[14px] text-ink leading-loose">
              {verdict.trial_summary}
            </p>
          </div>
        </section>
      )}

      {/* ============= 判决书正文（Markdown 渲染） ============= */}
      <section className="container mx-auto max-w-4xl px-6 pb-12">
        <div className="bg-white border border-rule shadow-paper p-8 md:p-12 relative">
          <div className="absolute top-0 left-0 right-0 h-px bg-judge" />
          <div className="text-center mb-8 pb-6 border-b border-rule">
            <p className="text-[10px] uppercase tracking-[0.3em] text-inkFaint font-data mb-2">
              DecisionCourt · Full Verdict
            </p>
            <h2 className="text-display text-2xl font-semibold text-ink">
              判 决 書 正 文
            </h2>
          </div>

          {contentBlocks.length > 0 ? (
            <MarkdownRenderer blocks={contentBlocks} />
          ) : (
            <div className="text-display text-[15px] text-ink leading-loose whitespace-pre-wrap">
              {verdict.content}
            </div>
          )}

          {/* 落款 */}
          <div className="mt-12 pt-6 border-t border-rule grid grid-cols-3 gap-6">
            <div className="text-center">
              <span className="seal-stamp w-12 h-12 text-base mx-auto flex items-center justify-center">
                判
              </span>
              <p className="text-display text-[10px] uppercase tracking-[0.25em] text-inkSoft font-data mt-2">
                法官印
              </p>
            </div>
            <div className="text-center">
              <div className="w-12 h-12 mx-auto border border-ink/40 rounded-sm flex items-center justify-center">
                <span className="text-display text-xs text-inkSoft">记</span>
              </div>
              <p className="text-display text-[10px] uppercase tracking-[0.25em] text-inkSoft font-data mt-2">
                书记员
              </p>
            </div>
            <div className="text-center">
              <div className="w-12 h-12 mx-auto border border-ink/40 rounded-sm flex items-center justify-center">
                <span className="text-display text-xs text-inkSoft">证</span>
              </div>
              <p className="text-display text-[10px] uppercase tracking-[0.25em] text-inkSoft font-data mt-2">
                归档
              </p>
            </div>
          </div>
        </div>
      </section>

      {/* ============= 庭审流程对话 ============= */}
      {transcript.length > 0 && (
        <section className="container mx-auto max-w-4xl px-6 pb-12">
          <div className="bg-white border border-rule shadow-paper p-6 md:p-8 relative">
            <div className="absolute top-0 left-0 right-0 h-px bg-defense" />
            <div className="flex items-baseline justify-between mb-5">
              <h3 className="text-display text-lg font-semibold text-ink flex items-center gap-2">
                <MessageSquare className="w-4 h-4 text-defense" />
                庭 审 流 程
              </h3>
              <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
                Trial Transcript · {transcript.length} 段
              </span>
            </div>

            <div className="space-y-4 max-h-[480px] overflow-y-auto pr-2">
              {transcript.map((item, i) => (
                <TranscriptItem key={i} item={item} index={i + 1} />
              ))}
            </div>
          </div>
        </section>
      )}

      {/* ============= v0.5 幕后视角（解锁）============= */}
      {/* Post-trial retrospective view of the v0.5 episodic-memory timeline.
          Always rendered (even when empty) so users learn that the feature
          exists for the next trial — see memory-a2a-redesign.md §PR 4. */}
      <section className="container mx-auto max-w-4xl px-6 pb-12">
        <BehindTheScenesPanel entries={memoryEntries} />
      </section>

      {/* ============= 反馈 + 操作 ============= */}
      <section className="container mx-auto max-w-4xl px-6 pb-16">
        <div className="bg-paperDeep border border-rule px-6 py-5 flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <span className="text-[10px] uppercase tracking-[0.25em] text-inkFaint font-data">
              判决反馈
            </span>
            <Button
              variant={feedback === "helpful" ? "default" : "outline"}
              size="sm"
              onClick={() => setFeedback("helpful")}
              className={
                feedback === "helpful"
                  ? "bg-defense text-paper hover:bg-defense-ink rounded-sm px-4 h-9 text-xs font-data tracking-wider"
                  : "bg-white border border-rule text-ink hover:bg-paper rounded-sm px-4 h-9 text-xs font-data tracking-wider"
              }
            >
              <ThumbsUp className="w-3.5 h-3.5 mr-1.5" />
              有 帮 助
            </Button>
            <Button
              variant={feedback === "not_helpful" ? "default" : "outline"}
              size="sm"
              onClick={() => setFeedback("not_helpful")}
              className={
                feedback === "not_helpful"
                  ? "bg-prosecution text-paper hover:bg-prosecution-ink rounded-sm px-4 h-9 text-xs font-data tracking-wider"
                  : "bg-white border border-rule text-ink hover:bg-paper rounded-sm px-4 h-9 text-xs font-data tracking-wider"
              }
            >
              <ThumbsDown className="w-3.5 h-3.5 mr-1.5" />
              没 帮 助
            </Button>
          </div>
          <Button
            onClick={() => router.push("/")}
            className="bg-ink text-paper hover:bg-inkSoft rounded-sm px-5 h-9 text-xs font-data tracking-wider"
          >
            <RotateCcw className="w-3.5 h-3.5 mr-1.5" />
            再 立 一 案
          </Button>
        </div>
      </section>

      {/* ============= 页脚 ============= */}
      <footer className="border-t border-rule">
        <div className="container mx-auto max-w-4xl px-6 py-5 flex items-center justify-between text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
          <span>DecisionCourt · 2026</span>
          <span className="flex items-center gap-1.5">
            <Scale className="w-3 h-3" />
            法庭即决策 · Decision-as-a-Verdict
          </span>
        </div>
      </footer>
    </div>
  );
}

/* --------------- 内部组件 --------------- */

function PointsList({
  points,
  tone,
}: {
  points: string[];
  tone: "prosecution" | "defense";
}) {
  const bulletColor = tone === "prosecution" ? "bg-prosecution" : "bg-defense";
  if (points.length === 0) {
    return (
      <p className="text-sm text-inkFaint italic text-display">
        暂无记录
      </p>
    );
  }
  return (
    <ul className="space-y-2.5">
      {points.map((p, i) => (
        <li
          key={i}
          className="text-[13px] text-ink leading-relaxed text-display flex gap-3"
        >
          <span
            className={`mt-2 w-1.5 h-1.5 rounded-full ${bulletColor} flex-shrink-0`}
          />
          <span>{p}</span>
        </li>
      ))}
    </ul>
  );
}

/**
 * 极简 markdown 解析 — 支持：H1/H2/H3、表格、有序/无序列表、段落
 * 输出结构化 blocks，由 MarkdownRenderer 渲染
 */
type MdBlock =
  | { type: "h1"; text: string }
  | { type: "h2"; text: string }
  | { type: "h3"; text: string }
  | { type: "p"; text: string }
  | { type: "ul"; items: string[] }
  | { type: "ol"; items: string[] }
  | { type: "table"; rows: string[][] };

function parseMarkdown(content: string): MdBlock[] {
  if (!content) return [];
  const lines = content.split("\n");
  const blocks: MdBlock[] = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];

    // H1
    if (/^#\s+/.test(line)) {
      blocks.push({ type: "h1", text: line.replace(/^#\s+/, "").trim() });
      i++;
      continue;
    }
    // H2
    if (/^##\s+/.test(line)) {
      blocks.push({ type: "h2", text: line.replace(/^##\s+/, "").trim() });
      i++;
      continue;
    }
    // H3
    if (/^###\s+/.test(line)) {
      blocks.push({ type: "h3", text: line.replace(/^###\s+/, "").trim() });
      i++;
      continue;
    }
    // 表格
    if (/^\|/.test(line) && i + 1 < lines.length && /^\|[\s\-:|]+\|/.test(lines[i + 1])) {
      const rows: string[][] = [];
      // header
      rows.push(
        line.split("|").slice(1, -1).map((c) => c.trim())
      );
      i += 2; // skip header + separator
      while (i < lines.length && /^\|/.test(lines[i]) && lines[i].trim() !== "") {
        rows.push(
          lines[i].split("|").slice(1, -1).map((c) => c.trim())
        );
        i++;
      }
      blocks.push({ type: "table", rows });
      continue;
    }
    // 无序列表
    if (/^[-*]\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^[-*]\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^[-*]\s+/, "").trim());
        i++;
      }
      blocks.push({ type: "ul", items });
      continue;
    }
    // 有序列表
    if (/^\d+\.\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\d+\.\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\d+\.\s+/, "").trim());
        i++;
      }
      blocks.push({ type: "ol", items });
      continue;
    }
    // 空行 — 跳过
    if (line.trim() === "") {
      i++;
      continue;
    }
    // 段落 — 收集直到空行或下一个块元素
    const buf: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() !== "" &&
      !/^[#|\-*]\s+|^\d+\.\s+/.test(lines[i]) &&
      !(/^\|/.test(lines[i]) && i + 1 < lines.length && /^\|[\s\-:|]+\|/.test(lines[i + 1]))
    ) {
      buf.push(lines[i]);
      i++;
    }
    if (buf.length > 0) {
      blocks.push({ type: "p", text: buf.join("\n").trim() });
    }
  }

  return blocks;
}

function renderInline(text: string): React.ReactNode[] {
  // 支持 **bold** 和 *italic*
  const parts: React.ReactNode[] = [];
  const re = /(\*\*([^*]+)\*\*|\*([^*]+)\*)/g;
  let last = 0;
  let m: RegExpExecArray | null;
  let key = 0;
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) parts.push(text.slice(last, m.index));
    if (m[2]) {
      parts.push(<strong key={key++} className="font-semibold text-ink">{m[2]}</strong>);
    } else if (m[3]) {
      parts.push(<em key={key++} className="italic">{m[3]}</em>);
    }
    last = m.index + m[0].length;
  }
  if (last < text.length) parts.push(text.slice(last));
  return parts;
}

function MarkdownRenderer({ blocks }: { blocks: MdBlock[] }) {
  return (
    <div className="space-y-6">
      {blocks.map((b, i) => {
        switch (b.type) {
          case "h1":
            return (
              <h2 key={i} className="text-display text-2xl font-semibold text-ink border-b border-rule pb-2">
                {b.text}
              </h2>
            );
          case "h2":
            return (
              <h3 key={i} className="text-display text-lg font-semibold text-ink mt-2 mb-3 flex items-baseline gap-3">
                <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
                  {String(i + 1).padStart(2, "0")}
                </span>
                <span>{b.text}</span>
              </h3>
            );
          case "h3":
            return (
              <h4 key={i} className="text-display text-base font-semibold text-ink mt-4 mb-2">
                {b.text}
              </h4>
            );
          case "p":
            return (
              <p key={i} className="text-display text-[15px] text-ink leading-loose">
                {renderInline(b.text)}
              </p>
            );
          case "ul":
            return (
              <ul key={i} className="space-y-1.5 pl-5 list-disc marker:text-inkFaint">
                {b.items.map((item, j) => (
                  <li key={j} className="text-display text-[15px] text-ink leading-relaxed">
                    {renderInline(item)}
                  </li>
                ))}
              </ul>
            );
          case "ol":
            return (
              <ol key={i} className="space-y-1.5 pl-5 list-decimal marker:text-inkFaint marker:font-data">
                {b.items.map((item, j) => (
                  <li key={j} className="text-display text-[15px] text-ink leading-relaxed">
                    {renderInline(item)}
                  </li>
                ))}
              </ol>
            );
          case "table":
            return <MdTable key={i} rows={b.rows} />;
        }
      })}
    </div>
  );
}

function MdTable({ rows }: { rows: string[][] }) {
  if (rows.length === 0) return null;
  const [header, ...body] = rows;
  return (
    <div className="my-4 border border-rule rounded-sm overflow-hidden">
      <table className="w-full border-collapse">
        <thead>
          <tr className="bg-paperDeep">
            {header.map((cell, i) => (
              <th
                key={i}
                className="text-left px-4 py-2 text-display text-[13px] font-semibold text-ink border-r border-rule last:border-r-0"
              >
                {cell}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {body.map((row, ri) => (
            <tr
              key={ri}
              className="border-t border-rule even:bg-paper/40"
            >
              {row.map((cell, ci) => (
                <td
                  key={ci}
                  className="px-4 py-2 text-display text-[14px] text-ink leading-relaxed align-top border-r border-rule last:border-r-0"
                >
                  {renderInline(cell)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * 把庭审消息按角色分组整理
 */
type TranscriptItem = {
  speaker: string;
  role: "prosecutor" | "defender" | "investigator" | "clerk" | "judge" | "user" | "system";
  content: string;
  phase?: string;
  round?: number;
};

const roleMeta: Record<TranscriptItem["role"], { color: string; label: string }> = {
  prosecutor:   { color: "bg-prosecution",   label: "控方" },
  defender:     { color: "bg-defense",       label: "辩方" },
  investigator: { color: "bg-neutral",      label: "调查员" },
  clerk:        { color: "bg-judge",        label: "书记员" },
  judge:        { color: "bg-judge",        label: "法官" },
  user:         { color: "bg-ink",          label: "你" },
  system:       { color: "bg-inkFaint",     label: "系统" },
};

function groupTranscript(messages: Message[]): TranscriptItem[] {
  const out: TranscriptItem[] = [];
  for (const m of messages ?? []) {
    const role = (m.agent_type ?? "system") as TranscriptItem["role"];
    out.push({
      role,
      speaker: roleMeta[role]?.label ?? "系统",
      content: m.content ?? "",
      phase: m.phase,
      round: m.round,
    });
  }
  return out;
}

function TranscriptItem({ item, index }: { item: TranscriptItem; index: number }) {
  const meta = roleMeta[item.role] ?? roleMeta.system;
  return (
    <div className="flex gap-3">
      <div className="shrink-0 w-7 flex flex-col items-center pt-1">
        <span
          className={`w-6 h-6 rounded-full ${meta.color} text-paper text-[11px] flex items-center justify-center font-display font-semibold`}
        >
          {String(index).padStart(2, "0").slice(-1)}
        </span>
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-baseline gap-2 mb-1">
          <span className="text-display text-[13px] font-semibold text-ink">
            {meta.label}
          </span>
          {item.phase && item.phase !== "opening" && item.phase !== "closing" && (
            <span className="text-[10px] uppercase tracking-[0.15em] text-inkFaint font-data">
              {item.phase}{item.round && item.round > 0 ? ` · 第 ${item.round} 轮` : ""}
            </span>
          )}
        </div>
        <p className="text-display text-[13.5px] text-ink leading-relaxed">
          {item.content}
        </p>
      </div>
    </div>
  );
}
