import type { Config } from "tailwindcss";

const config: Config = {
  darkMode: ["class"],
  content: [
    "./pages/**/*.{js,ts,jsx,tsx,mdx}",
    "./components/**/*.{js,ts,jsx,tsx,mdx}",
    "./app/**/*.{js,ts,jsx,tsx,mdx}",
  ],
  theme: {
    extend: {
      // 案卷·印章 设计 tokens
      colors: {
        // 纸色体系
        paper: "#FAF8F4",
        paperDeep: "#F2EDE3",
        rule: "#E8E2D5",
        // 墨色体系
        ink: "#1A1815",
        inkSoft: "#5C564F",
        inkFaint: "#A89F8E",
        // 角色色（区别于 AI 默认蓝/红）
        prosecution: {
          DEFAULT: "#B53A2E",
          soft: "#E8C8C3",
          ink: "#7A1F18",
        },
        defense: {
          DEFAULT: "#2C5470",
          soft: "#BFCED9",
          ink: "#16334A",
        },
        judge: {
          DEFAULT: "#7A5C3F",
          soft: "#D9C9B5",
          ink: "#3F2E1E",
        },
        neutral: {
          DEFAULT: "#A89F8E",
          soft: "#E5E0D5",
        },
        // 印章
        seal: {
          DEFAULT: "#C8342A",
          ink: "#4A0F0A",
        },
        // 金色点缀
        gold: "#D4A24C",
        // shadcn 兼容
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
      },
      fontFamily: {
        // 案卷·印章 字体家族
        serif: ["var(--font-serif)", "Noto Serif SC", "Georgia", "serif"],
        body: ["var(--font-body)", "Noto Sans SC", "system-ui", "sans-serif"],
        display: ["var(--font-serif)", "Noto Serif SC", "ZCOOL XiaoWei", "Georgia", "serif"],
        data: ["var(--font-mono)", "JetBrains Mono", "ui-monospace", "monospace"],
      },
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
      },
      // 案卷阴影（更克制）
      boxShadow: {
        paper: "0 1px 0 rgba(26,24,21,0.04), 0 1px 3px rgba(26,24,21,0.06)",
        "paper-lg": "0 1px 2px rgba(26,24,21,0.04), 0 4px 12px rgba(26,24,21,0.06)",
        seal: "0 2px 0 #4A0F0A, 0 4px 8px rgba(74,15,10,0.3)",
      },
      // 案卷纹理
      backgroundImage: {
        "paper-grain":
          "radial-gradient(circle at 25% 25%, rgba(168,159,142,0.04) 1px, transparent 1px), radial-gradient(circle at 75% 75%, rgba(168,159,142,0.04) 1px, transparent 1px)",
      },
      backgroundSize: {
        "paper-grain": "32px 32px",
      },
      animation: {
        // 仅保留印章动效，移除所有 ripple（避免 AI 默认的"杂多动效"感）
        sealDrop: "sealDrop 0.6s cubic-bezier(0.34, 1.56, 0.64, 1) forwards",
      },
      keyframes: {
        sealDrop: {
          "0%": { transform: "rotate(-30deg) scale(2)", opacity: "0" },
          "60%": { transform: "rotate(-8deg) scale(1.05)", opacity: "1" },
          "80%": { transform: "rotate(2deg) scale(0.98)" },
          "100%": { transform: "rotate(0deg) scale(1)", opacity: "1" },
        },
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
};

export default config;
