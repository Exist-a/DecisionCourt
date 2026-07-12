import type { Metadata } from "next";
import localFont from "next/font/local";
import "./globals.css";
import { cn } from "@/lib/utils";
import { ToastContainer } from "@/components/ui/Toast";

const geistSans = localFont({
  src: "./fonts/GeistVF.woff",
  variable: "--font-geist-sans",
  weight: "100 900",
});

const geistMono = localFont({
  src: "./fonts/GeistMonoVF.woff",
  variable: "--font-geist-mono",
  weight: "100 900",
});

// v0.9.1 (P0 部署):中文字体 fallback。
// 之前 layout.tsx 引了 fonts.googleapis.com → CSP 拒 + 隐私问题。
// next/font/google 又会在 build 时拉 fonts.gstatic.com → 国内 TLS 拦截。
// 折衷方案:只用 Geist Sans(latin),中文走 system fallback
// (macOS 苹方 / Windows 微软雅黑 / Linux Noto Sans CJK)。
// 中文 UI 略丑但能稳定 build + 离线运行,等上线稳定再考虑字体自托管。
export const metadata: Metadata = {
  title: "决策庭 DecisionCourt",
  description: "多 Agent 法庭式决策辅助平台",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-CN" className={cn("font-body")}>
      <body
        className={`${geistSans.variable} ${geistMono.variable} font-body antialiased bg-paper text-ink`}
      >
        {children}
        {/* v0.10.17 silent-error-fix: 全局 Toast 容器,
            自动渲染 store 中的 toast(右下角堆叠) + degraded banner(顶部)。
            任何组件 import useToastStore().push 即可弹 toast,无需手动挂。 */}
        <ToastContainer />
      </body>
    </html>
  );
}
