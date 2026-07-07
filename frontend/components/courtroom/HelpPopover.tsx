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
          {/* 段 1:顶部右侧按钮 */}
          <div>
            <h5 className="font-medium text-slate-900">右上角的按钮</h5>
            <ul className="mt-1 space-y-1 text-slate-600">
              <li>
                <span className="font-medium text-slate-800">「开 庭」</span>
                :开始一场庭审。
              </li>
              <li>
                <span className="font-medium text-slate-800">「直接判决」</span>
                :跳过剩余轮次,直接看判决。
              </li>
              <li>
                <span className="font-medium text-slate-800">「查看判决書」</span>
                :庭审已结束,查看结果。
              </li>
            </ul>
          </div>

          {/* 段 2:底部操作 */}
          <div className="border-t border-slate-100 pt-2">
            <h5 className="font-medium text-slate-900">底部的操作</h5>
            <ul className="mt-1 space-y-1 text-slate-600">
              <li>
                <span className="font-medium text-slate-800">「归 档 证 据」</span>
                :提交一条结构化证据(事实 / 数据 / 偏好 / 约束)。
              </li>
              <li>
                <span className="font-medium text-slate-800">输入框 + 发送按钮</span>
                :随时补充证据或追加观点,回车即可发送。
              </li>
              <li>
                <span className="font-medium text-slate-800">红印章按钮</span>
                :质证阶段切换时出现,点击进入下一轮。
              </li>
            </ul>
          </div>

          {/* 段 3:右侧面板 */}
          <div className="border-t border-slate-100 pt-2">
            <h5 className="font-medium text-slate-900">右侧四个面板</h5>
            <ul className="mt-1 space-y-1 text-slate-600">
              <li>庭审记录:完整对话流。</li>
              <li>调查活动:调查员搜到的资料。</li>
              <li>策略笔记:每位律师的内心想法。</li>
              <li>信念轨迹:各方观点如何变化。</li>
            </ul>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
