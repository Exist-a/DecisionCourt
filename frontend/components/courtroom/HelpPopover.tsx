"use client";

import { HelpCircle } from "lucide-react";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

export function HelpPopover() {
  return (
    <Popover>
      <PopoverTrigger className="h-8 w-8 inline-flex items-center justify-center rounded-full text-slate-400 hover:text-slate-600 hover:bg-slate-100 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <HelpCircle className="w-4 h-4" />
      </PopoverTrigger>
      <PopoverContent
        align="end"
        className="w-80 bg-white border-slate-200 text-slate-700 text-sm"
      >
        <div className="space-y-3">
          <h4 className="font-semibold text-slate-900">庭审流程</h4>
          <ol className="space-y-1.5 list-decimal list-inside text-slate-600">
            <li>立案：输入决策问题、两个选项和背景。</li>
            <li>开庭：控辩双方分别做开场陈述。</li>
            <li>举证/质证：多轮攻防，你可随时提交证据。</li>
            <li>调查：让调查员搜索外部信息作为证据。</li>
            <li>结案：双方总结后书记员生成判决书。</li>
          </ol>
          <div className="border-t border-slate-100 pt-2">
            <h5 className="font-medium text-slate-900 mb-1">你的操作</h5>
            <ul className="space-y-1 text-slate-600">
              <li>「+ 证据」：提交事实、数据、偏好或约束。</li>
              <li>「调查」：让调查员按关键词搜索。</li>
              <li>「打断」：停止当前 Agent，输入内容让其重新考虑。</li>
              <li>「跳过」：跳过当前 Agent 发言。</li>
              <li>「直接判决」：立即结束庭审生成判决书。</li>
            </ul>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
