"use client";

// v1.0-patch (2026-08-22): 修浏览器 back bug
//
// 触发: 用户从 /verdict/<uuid> 点浏览器 back 跳到 /court/<uuid> 时,
//       庭审现场显示"无 session / 无 agents / 无 evidence" → 大量 404。
// 根因: court page 之前直接渲染 <CourtroomScene/>, 没有 hydrate store 逻辑,
//       假设 store 已被实时 WS 填好 (浏览器 back 不重发 WS,假设失败)。
// 修复: 仿 verdict/[id]/page.tsx 模式, mount 时调共享 hydrate 函数。

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { CourtroomScene } from "@/components/courtroom/CourtroomScene";
import { useCourtroomStore } from "@/store/courtroomStore";
import { hydrateCourtroomStore } from "@/lib/courtroomHydrate";
import { Loader2 } from "lucide-react";

export default function CourtPage() {
  const params = useParams();
  const sessionId = params.id as string;
  const [hydrated, setHydrated] = useState(false);

  // 共享 store setter (从 courtroomStore 取)
  const setSession = useCourtroomStore((s) => s.setSession);
  const setAgents = useCourtroomStore((s) => s.setAgents);
  const addEvidence = useCourtroomStore((s) => s.addEvidence);
  const setInvestigationFindings = useCourtroomStore(
    (s) => s.setInvestigationFindings,
  );
  const setBeliefDiffs = useCourtroomStore((s) => s.setBeliefDiffs);
  const setMemoryEntries = useCourtroomStore((s) => s.setMemoryEntries);

  useEffect(() => {
    let mounted = true;
    void (async () => {
      await hydrateCourtroomStore(sessionId, {
        setSession,
        setAgents,
        addEvidence,
        setInvestigationFindings,
        setBeliefDiffs,
        // 幂等去重要求:每次 hydrate 前看 store 当前内容
        getStoredEvidences: () => useCourtroomStore.getState().evidences,
        setMemoryEntries,
      });
      if (mounted) setHydrated(true);
    })();
    return () => {
      mounted = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  // hydrate 期间显示 loading (与 verdict 页 loading 状态一致)
  if (!hydrated) {
    return (
      <div className="h-screen flex items-center justify-center bg-paper text-ink">
        <div className="flex items-center gap-3 text-stone-500">
          <Loader2 className="w-5 h-5 animate-spin" />
          <span className="text-sm font-data">正在加载庭审现场…</span>
        </div>
      </div>
    );
  }

  return <CourtroomScene sessionId={sessionId} />;
}
