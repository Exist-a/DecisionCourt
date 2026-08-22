// v1.0-patch (2026-08-22) PR-2: 历史庭审列表 localStorage 管理
//
// 用途:
//   - 首页 TrialHistoryList 组件读 trialHistory 渲染历史庭审
//   - 用户立案成功 → saveHistoryItem (新增/更新)
//   - 用户点"删除" → removeHistoryItem
//   - 启动时 syncFromServer (从后端 GET /courtrooms 同步, 跨设备同步)
//
// 复用 frontend/lib/memoryCache.ts 的 SSR-safe 模式 + STORAGE_KEY_PREFIX 命名规范。
//
// 设计取舍:
//   - localStorage (而非 sessionStorage): 跨刷新仍可用
//   - 按 updated_at DESC 排序, FIFO 淘汰最旧 (容量上限 50 条)
//   - 序列化: JSON.stringify, 失败 (quota exceeded / SSR) 静默吞错
//   - 软删除: localStorage 删即可, DB 数据保留 (供未来 rejoin)

import type { CourtPhase } from "@/types";

const STORAGE_KEY = "dc_trial_history_v1";
const MAX_ENTRIES = 50;

// TrialHistoryItem: 用于前端"历史庭审"列表渲染的 session 元信息
//
// 字段映射 (后端 GET /courtrooms response.data.sessions[]):
//   - sessionUUID → session_uuid
//   - title       → title
//   - optionA     → option_a
//   - optionB     → option_b
//   - currentPhase → current_phase
//   - verdictReady = (current_phase === "verdict" 或 "deliberation")
//   - updatedAt   → updated_at (Date.parse ISO string)
export interface TrialHistoryItem {
	sessionUUID: string;
	title: string;
	optionA: string;
	optionB: string;
	currentPhase: CourtPhase | string; // 兼容未知 phase 字符串
	verdictReady: boolean;
	updatedAt: number; // Date.now() — 用于排序 + 显示
}

function hasLocalStorage(): boolean {
	try {
		return typeof window !== "undefined" && !!window.localStorage;
	} catch {
		return false;
	}
}

/**
 * 从 localStorage 读全部历史庭审。返回空数组表示无 / SSR / 损坏。
 */
export function loadHistory(): TrialHistoryItem[] {
	if (!hasLocalStorage()) return [];
	try {
		const raw = window.localStorage.getItem(STORAGE_KEY);
		if (!raw) return [];
		const parsed = JSON.parse(raw) as TrialHistoryItem[];
		if (!Array.isArray(parsed)) return [];
		// 按 updatedAt DESC 排序 (FIFO 淘汰后, 新加载仍有序)
		return parsed.sort((a, b) => b.updatedAt - a.updatedAt);
	} catch (err) {
		console.warn("[trialHistory] load failed:", err);
		return [];
	}
}

/**
 * 增量保存: 新增或更新一条 (按 sessionUUID 去重), 重新排序 + 容量淘汰。
 */
export function saveHistoryItem(item: TrialHistoryItem): void {
	if (!hasLocalStorage()) return;
	try {
		const items = loadHistory();
		const idx = items.findIndex((x) => x.sessionUUID === item.sessionUUID);
		if (idx >= 0) {
			items[idx] = item; // 更新
		} else {
			items.unshift(item); // 插到最前
		}
		// 按 updatedAt DESC 排序 + 容量淘汰 (FIFO 删最旧)
		const trimmed = items
			.sort((a, b) => b.updatedAt - a.updatedAt)
			.slice(0, MAX_ENTRIES);
		window.localStorage.setItem(STORAGE_KEY, JSON.stringify(trimmed));
	} catch (err) {
		console.warn("[trialHistory] save failed:", err);
	}
}

/**
 * 删除单条 (用户点 × 按钮)。
 */
export function removeHistoryItem(sessionUUID: string): void {
	if (!hasLocalStorage()) return;
	try {
		const items = loadHistory();
		const filtered = items.filter((x) => x.sessionUUID !== sessionUUID);
		if (filtered.length !== items.length) {
			window.localStorage.setItem(STORAGE_KEY, JSON.stringify(filtered));
		}
	} catch (err) {
		console.warn("[trialHistory] remove failed:", err);
	}
}

/**
 * 启动时从后端 sync: GET /api/v1/courtrooms 返回的列表覆盖 localStorage。
 *
 * 触发: TrialHistoryList mount 时调一次, 保证首页显示的列表与 server 同步
 * (用户换了设备 / 别人用了同一账号, 都能看到最新历史)。
 *
 * @param serverItems - 后端 GET /courtrooms 返回的 session 列表 (已是 gin.H 格式)
 * @returns 合并后的新数组
 */
export function syncFromServer(
	serverItems: Array<{
		session_uuid: string;
		title: string;
		option_a: string;
		option_b: string;
		current_phase: string;
		updated_at: string;
	}>,
): TrialHistoryItem[] {
	if (!hasLocalStorage()) return [];

	const merged: TrialHistoryItem[] = serverItems.map((s) => ({
		sessionUUID: s.session_uuid,
		title: s.title,
		optionA: s.option_a,
		optionB: s.option_b,
		currentPhase: s.current_phase,
		verdictReady: s.current_phase === "verdict" || s.current_phase === "deliberation",
		updatedAt: Date.parse(s.updated_at) || Date.now(),
	}));

	try {
		window.localStorage.setItem(STORAGE_KEY, JSON.stringify(merged));
	} catch (err) {
		console.warn("[trialHistory] sync save failed:", err);
	}
	return merged;
}
