"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { api } from "@/lib/api";
import { useCourtroomStore } from "@/store/courtroomStore";
import {
  Scale,
  MessageSquare,
  FileText,
  Sparkles,
  BookOpen,
  ChevronRight,
} from "lucide-react";

export default function Home() {
  const router = useRouter();
  const reset = useCourtroomStore((s) => s.reset);
  const setSession = useCourtroomStore((s) => s.setSession);
  const [loading, setLoading] = useState(false);
  const [title, setTitle] = useState("");
  const [optionA, setOptionA] = useState("");
  const [optionB, setOptionB] = useState("");
  const [context, setContext] = useState("");
  const [mode, setMode] = useState<"quick" | "standard" | "deep">("standard");

  const modeLabels: Record<typeof mode, string> = {
    quick: "快速模式 · 二轮庭审",
    standard: "标准模式 · 三轮庭审",
    deep: "深度模式 · 五轮庭审",
  };

  const modeDesc: Record<typeof mode, string> = {
    quick: "10 分钟拿结论",
    standard: "20 分钟看全貌",
    deep: "45 分钟彻底剖析",
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!title.trim()) return;

    setLoading(true);
    reset();

    try {
      const res = await api.createSession({
        title: title.trim(),
        option_a: optionA.trim() || undefined,
        option_b: optionB.trim() || undefined,
        context: context.trim() || undefined,
        mode,
      });

      if (res.code === 0) {
        setSession(res.data);
        router.push(`/court/${res.data.session_uuid}`);
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-paper text-ink paper-overlay">
      {/* ============ 顶部封面 ============ */}
      <header className="border-b border-rule">
        <div className="container mx-auto max-w-5xl px-6 py-4 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <span className="seal-stamp w-9 h-9 text-[15px] flex items-center justify-center">
              判
            </span>
            <div>
              <h2 className="text-display text-base font-semibold text-ink leading-tight">
                决 策 庭
              </h2>
              <p className="text-[10px] uppercase tracking-[0.25em] text-inkFaint font-data leading-tight mt-0.5">
                DecisionCourt
              </p>
            </div>
          </div>
          <span className="hidden md:flex items-center gap-2 text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
            <BookOpen className="w-3 h-3" />
            立 案 · 庭 审 · 判 决
          </span>
        </div>
      </header>

      {/* ============ Hero ============ */}
      <section className="container mx-auto max-w-5xl px-6 pt-16 pb-10">
        <div className="text-center">
          {/* 案件编号水印 */}
          {/* v0.9.1 修复：日期在 render 期间由 new Date() 生成,服务端用 UTC、
              客户端(Asia/Shanghai UTC+8)在跨日 UTC 时刻可能差一天 → #425。
              suppressHydrationWarning 是 React 推荐的"时区/时间不一致"场景,
              让客户端值覆盖服务端值,不抛 hydration error。 */}
          <p
            className="text-[10px] uppercase tracking-[0.3em] text-inkFaint font-data mb-5"
            suppressHydrationWarning
          >
            Case No. DC-{new Date().getFullYear()}
            {(new Date().getMonth() + 1).toString().padStart(2, "0")}
            {new Date().getDate().toString().padStart(2, "0")} · 立 案 申 请
          </p>

          <h1 className="text-display text-4xl md:text-5xl font-semibold text-ink leading-tight mb-4">
            让 AI Agent 像法庭一样
            <br />
            帮你把决策
            <span className="relative inline-block px-2 mx-1">
              <span className="relative z-10">看全</span>
              <span className="absolute bottom-1 left-0 right-0 h-2 bg-judge/20 -z-0" />
            </span>
            ·
            <span className="relative inline-block px-2 mx-1">
              <span className="relative z-10">看透</span>
              <span className="absolute bottom-1 left-0 right-0 h-2 bg-prosecution/20 -z-0" />
            </span>
            ·
            <span className="relative inline-block px-2 mx-1">
              <span className="relative z-10">看出可执行结论</span>
              <span className="absolute bottom-1 left-0 right-0 h-2 bg-defense/20 -z-0" />
            </span>
          </h1>

          <p className="text-display text-base text-inkSoft max-w-2xl mx-auto leading-relaxed">
            控方、辩方、调查员、书记员四方代表将就你的问题展开对抗式辩论。
            <br />
            你作为主审法官，可随时补充证据、调取资料、做出最终判决。
          </p>
        </div>
      </section>

      {/* ============ 立案表单（案卷封面） ============ */}
      <section className="container mx-auto max-w-5xl px-6 pb-16">
        <form
          onSubmit={handleSubmit}
          className="bg-white border border-rule shadow-paper-lg relative"
        >
          {/* 卷宗签条 */}
          <div className="absolute -top-3 left-8 phase-ribbon">
            立案申请书
          </div>

          <div className="p-8 md:p-10 space-y-7">
            {/* 决策问题 */}
            <div className="space-y-2">
              <Label className="text-[10px] uppercase tracking-[0.2em] text-inkSoft font-data flex items-center gap-2">
                <span className="inline-block w-3 h-px bg-prosecution" />
                你的决策问题
                <span className="text-[9px] text-prosecution">*</span>
              </Label>
              <Input
                placeholder="例如：我该跳槽去创业公司还是留在大厂？"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                className="bg-paper border border-rule rounded-sm h-14 text-lg text-display focus:bg-white"
                required
              />
            </div>

            {/* 当事人两栏 */}
            <div className="space-y-2">
              <Label className="text-[10px] uppercase tracking-[0.2em] text-inkSoft font-data flex items-center gap-2">
                <span className="inline-block w-3 h-px bg-defense" />
                当 事 人 意 见
                <span className="text-[9px] text-inkFaint normal-case tracking-normal">
                  (选填 · 不填将由 Agent 协助生成)
                </span>
              </Label>

              <div className="grid md:grid-cols-[1fr_auto_1fr] gap-0 border border-rule rounded-sm overflow-hidden">
                {/* 控方（选项 A） */}
                <div className="bg-paper p-5 border-r border-prosecution/30 relative">
                  <div className="absolute top-0 left-0 right-0 h-1 bg-prosecution" />
                  <div className="flex items-center justify-between mb-3">
                    <span className="text-display text-sm font-semibold text-prosecution-ink">
                      控方主张 · 选 项 A
                    </span>
                    <span className="text-[10px] uppercase tracking-[0.15em] text-prosecution font-data">
                      Prosecution
                    </span>
                  </div>
                  <Input
                    placeholder="接受创业公司 offer"
                    value={optionA}
                    onChange={(e) => setOptionA(e.target.value)}
                    className="bg-white border border-rule rounded-sm h-11 text-display"
                  />
                </div>

                {/* 中间分隔 */}
                <div className="flex items-center justify-center px-2 bg-paperDeep">
                  <div className="text-display text-inkFaint text-2xl font-light">
                    ⚖
                  </div>
                </div>

                {/* 辩方（选项 B） */}
                <div className="bg-paper p-5 relative">
                  <div className="absolute top-0 left-0 right-0 h-1 bg-defense" />
                  <div className="flex items-center justify-between mb-3">
                    <span className="text-display text-sm font-semibold text-defense-ink">
                      辩方主张 · 选 项 B
                    </span>
                    <span className="text-[10px] uppercase tracking-[0.15em] text-defense font-data">
                      Defense
                    </span>
                  </div>
                  <Input
                    placeholder="留在现在的大厂"
                    value={optionB}
                    onChange={(e) => setOptionB(e.target.value)}
                    className="bg-white border border-rule rounded-sm h-11 text-display"
                  />
                </div>
              </div>
            </div>

            {/* 背景信息 */}
            <div className="space-y-2">
              <Label className="text-[10px] uppercase tracking-[0.2em] text-inkSoft font-data flex items-center gap-2">
                <span className="inline-block w-3 h-px bg-judge" />
                案 情 背 景
              </Label>
              <Textarea
                placeholder="补充你的背景、约束、偏好，帮助 Agent 更精准地辩论……"
                value={context}
                onChange={(e) => setContext(e.target.value)}
                className="bg-paper border border-rule rounded-sm min-h-[100px] resize-none text-display focus:bg-white"
              />
            </div>

            {/* 庭审模式选择 */}
            <div className="grid md:grid-cols-[1fr_auto] gap-5 items-end pt-2 border-t border-rule">
              <div className="space-y-2">
                <Label className="text-[10px] uppercase tracking-[0.2em] text-inkSoft font-data flex items-center gap-2">
                  <span className="inline-block w-3 h-px bg-ink" />
                  庭 审 模 式
                </Label>
                <Select
                  value={mode}
                  onValueChange={(v) =>
                    setMode(v as "quick" | "standard" | "deep")
                  }
                >
                  <SelectTrigger className="bg-paper border border-rule rounded-sm h-12 text-display">
                    <SelectValue>{modeLabels[mode]}</SelectValue>
                  </SelectTrigger>
                  <SelectContent className="bg-white border-rule">
                    <SelectItem value="quick">
                      <div className="flex flex-col">
                        <span className="text-display">快速模式 · 二轮庭审</span>
                        <span className="text-[10px] text-inkFaint">
                          {modeDesc.quick}
                        </span>
                      </div>
                    </SelectItem>
                    <SelectItem value="standard">
                      <div className="flex flex-col">
                        <span className="text-display">标准模式 · 三轮庭审</span>
                        <span className="text-[10px] text-inkFaint">
                          {modeDesc.standard}
                        </span>
                      </div>
                    </SelectItem>
                    <SelectItem value="deep">
                      <div className="flex flex-col">
                        <span className="text-display">深度模式 · 五轮庭审</span>
                        <span className="text-[10px] text-inkFaint">
                          {modeDesc.deep}
                        </span>
                      </div>
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <Button
                type="submit"
                size="lg"
                disabled={loading}
                className="h-12 rounded-sm bg-ink text-paper hover:bg-inkSoft px-6 font-data tracking-wider text-sm"
              >
                {loading ? "立 案 中 …" : "立 案 · 开 庭"}
                <ChevronRight className="w-4 h-4 ml-1" />
              </Button>
            </div>
          </div>
        </form>
      </section>

      {/* ============ 三大特性（案卷夹三栏） ============ */}
      <section className="container mx-auto max-w-5xl px-6 pb-20">
        <div className="text-center mb-8">
          <p className="text-[10px] uppercase tracking-[0.3em] text-inkFaint font-data">
            为 何 选 择 决 策 庭
          </p>
        </div>

        <div className="grid md:grid-cols-3 gap-5">
          {/* 特性 1 */}
          <article className="bg-white border border-rule shadow-paper p-6 relative group">
            <div className="absolute top-0 left-0 right-0 h-px bg-prosecution" />
            <div className="flex items-start gap-3 mb-3">
              <div className="w-9 h-9 rounded-sm bg-prosecution/10 flex items-center justify-center shrink-0">
                <MessageSquare className="w-4 h-4 text-prosecution" />
              </div>
              <div>
                <p className="text-[9px] uppercase tracking-[0.25em] text-inkFaint font-data">
                  Multi-Agent
                </p>
                <h3 className="text-display text-base font-semibold text-ink mt-0.5">
                  多 Agent 对抗
                </h3>
              </div>
            </div>
            <p className="text-sm text-inkSoft leading-relaxed text-display">
              控辩双方基于证据链攻防，避免单一 AI 的片面建议。
              <span className="text-inkFaint italic"> — 让偏见无处藏身</span>
            </p>
          </article>

          {/* 特性 2 */}
          <article className="bg-white border border-rule shadow-paper p-6 relative group">
            <div className="absolute top-0 left-0 right-0 h-px bg-defense" />
            <div className="flex items-start gap-3 mb-3">
              <div className="w-9 h-9 rounded-sm bg-defense/10 flex items-center justify-center shrink-0">
                <Sparkles className="w-4 h-4 text-defense" />
              </div>
              <div>
                <p className="text-[9px] uppercase tracking-[0.25em] text-inkFaint font-data">
                  Real-time
                </p>
                <h3 className="text-display text-base font-semibold text-ink mt-0.5">
                  实时插证据
                </h3>
              </div>
            </div>
            <p className="text-sm text-inkSoft leading-relaxed text-display">
              你作为法官可随时提交证据、传唤调查、打断质询。
              <span className="text-inkFaint italic"> — 法庭为你展开</span>
            </p>
          </article>

          {/* 特性 3 */}
          <article className="bg-white border border-rule shadow-paper p-6 relative group">
            <div className="absolute top-0 left-0 right-0 h-px bg-judge" />
            <div className="flex items-start gap-3 mb-3">
              <div className="w-9 h-9 rounded-sm bg-judge/10 flex items-center justify-center shrink-0">
                <FileText className="w-4 h-4 text-judge" />
              </div>
              <div>
                <p className="text-[9px] uppercase tracking-[0.25em] text-inkFaint font-data">
                  Verdict
                </p>
                <h3 className="text-display text-base font-semibold text-ink mt-0.5">
                  结构化判决
                </h3>
              </div>
            </div>
            <p className="text-sm text-inkSoft leading-relaxed text-display">
              输出双方主张、证据链、争议焦点与可执行建议。
              <span className="text-inkFaint italic"> — 落到行动的结论</span>
            </p>
          </article>
        </div>
      </section>

      {/* ============ 页脚 ============ */}
      <footer className="border-t border-rule">
        <div className="container mx-auto max-w-5xl px-6 py-5 flex items-center justify-between text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
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
