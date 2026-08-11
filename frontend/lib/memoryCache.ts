// D2-Memory (v0.10.x D2 silent-error-fix 收尾): localStorage 缓存
//
// 用途: GetVisibleMemory API 失败时, 降级到上次成功拉到的 memory 缓存。
// 复用 frontend/lib/auth.ts 的 STORAGE_KEY_xxx 常量模式 + hasLocalStorage() helper。
//
// 设计取舍:
//   - localStorage (而非 sessionStorage): 跨刷新仍可用
//   - 按 session_uuid 隔离缓存: 不同时段 memory 不混淆
//   - 序列化: JSON.stringify 整个 MemoryEntry 数组, 加 savedAt timestamp
//   - 容量保护: 单 session 缓存上限 100 条 (与 MemoryAuditPanel 渲染量匹配)

import type { MemoryEntry } from "@/types";

const STORAGE_KEY_PREFIX = "dc_mem_cache_";
const MAX_ENTRIES_PER_SESSION = 100;

interface CachedMemory {
	savedAt: string; // ISO-8601
	entries: MemoryEntry[];
}

function hasLocalStorage(): boolean {
	try {
		return typeof window !== "undefined" && !!window.localStorage;
	} catch {
		return false;
	}
}

function keyFor(sessionUUID: string): string {
	return `${STORAGE_KEY_PREFIX}${sessionUUID}`;
}

/**
 * 把 memory 列表保存到 localStorage。失败 (quota exceeded / SSR / 序列化错) 静默吞错,
 * 仿 auth.ts setAuthToken 模式 (silent warn 不抛)。
 */
export function saveMemoryCache(sessionUUID: string, entries: MemoryEntry[]): void {
	if (!hasLocalStorage()) return;
	try {
		const trimmed = entries.slice(-MAX_ENTRIES_PER_SESSION);
		const payload: CachedMemory = {
			savedAt: new Date().toISOString(),
			entries: trimmed,
		};
		window.localStorage.setItem(keyFor(sessionUUID), JSON.stringify(payload));
	} catch (err) {
		// localStorage quota exceeded / 序列化失败 → silent warn, 不阻断
		console.warn("[memoryCache] save failed:", err);
	}
}

/**
 * 从 localStorage 读缓存。返回 null 表示无缓存 / 缓存损坏。
 */
export function loadMemoryCache(sessionUUID: string): {
	savedAt: string;
	entries: MemoryEntry[];
} | null {
	if (!hasLocalStorage()) return null;
	try {
		const raw = window.localStorage.getItem(keyFor(sessionUUID));
		if (!raw) return null;
		const parsed = JSON.parse(raw) as CachedMemory;
		if (!parsed || !Array.isArray(parsed.entries)) return null;
		return parsed;
	} catch (err) {
		console.warn("[memoryCache] load failed:", err);
		return null;
	}
}

/**
 * 清空某个 session 的缓存 (用于 session 重置 / 退出登录)。
 */
export function clearMemoryCache(sessionUUID: string): void {
	if (!hasLocalStorage()) return;
	try {
		window.localStorage.removeItem(keyFor(sessionUUID));
	} catch (err) {
		console.warn("[memoryCache] clear failed:", err);
	}
}
