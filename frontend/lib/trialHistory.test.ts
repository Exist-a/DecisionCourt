// v1.0-patch (2026-08-22) PR-2: trialHistory 单元测试
//
// 2 sub-test 覆盖 (Node --test):
//   T1: loadHistory + saveHistoryItem 完整循环
//   T2: 容量淘汰 (超过 MAX_ENTRIES=50 时 FIFO 删最旧)
//
// 测试用 globalThis.localStorage 模拟 (与 lib/errorBus.test.ts 同模式)。

import { test } from "node:test";
import assert from "node:assert/strict";

import { loadHistory, saveHistoryItem, removeHistoryItem } from "./trialHistory.ts";

const KEY = "dc_trial_history_v1";

// 设置测试用 localStorage mock (Node --test 跑没有 window, 需要 mock)
function setupLocalStorage() {
	const store = new Map<string, string>();
	// @ts-expect-error -- mock globalThis.localStorage
	globalThis.localStorage = {
		getItem: (k: string) => store.get(k) ?? null,
		setItem: (k: string, v: string) => {
			store.set(k, v);
		},
		removeItem: (k: string) => {
			store.delete(k);
		},
		clear: () => store.clear(),
		key: (i: number) => Array.from(store.keys())[i] ?? null,
		get length() {
			return store.size;
		},
	};
	// @ts-expect-error -- mock window
	globalThis.window = { localStorage: globalThis.localStorage };
}

function makeItem(uuid: string, title: string, updatedAt: number) {
	return {
		sessionUUID: uuid,
		title,
		optionA: "A",
		optionB: "B",
		currentPhase: "verdict" as const,
		verdictReady: true,
		updatedAt,
	};
}

// T1: 完整循环 — save 3 条, load 返 3 条按 updatedAt DESC,
//     update 已有 (按 sessionUUID 去重), remove 单条。
test("trialHistory: save/load/update/remove 完整循环", () => {
	setupLocalStorage();
	globalThis.localStorage.clear();

	// 1. save 3 条
	saveHistoryItem(makeItem("uuid-1", "案 A", 1000));
	saveHistoryItem(makeItem("uuid-2", "案 B", 2000));
	saveHistoryItem(makeItem("uuid-3", "案 C", 3000));

	// 2. load 返 3 条按 updatedAt DESC
	const items = loadHistory();
	assert.equal(items.length, 3, "load 返 3 条");
	assert.equal(items[0].sessionUUID, "uuid-3", "DESC 第一应是 uuid-3 (3000)");
	assert.equal(items[1].sessionUUID, "uuid-2");
	assert.equal(items[2].sessionUUID, "uuid-1");

	// 3. update uuid-1 (updatedAt = 5000) — 应移到第 1
	saveHistoryItem(makeItem("uuid-1", "案 A 更新", 5000));
	const after = loadHistory();
	assert.equal(after.length, 3, "update 不增加条数");
	assert.equal(after[0].sessionUUID, "uuid-1", "updatedAt 5000 应排第 1");

	// 4. remove uuid-2
	removeHistoryItem("uuid-2");
	const removed = loadHistory();
	assert.equal(removed.length, 2, "remove 后剩 2 条");
	assert.equal(
		removed.find((x) => x.sessionUUID === "uuid-2"),
		undefined,
		"uuid-2 应被删",
	);
});

// T2: 容量淘汰 — 写 51 条, 期望保留最新 50 条 (FIFO 删最旧的 1 条)
test("trialHistory: 超过 MAX_ENTRIES 时 FIFO 删最旧", () => {
	setupLocalStorage();
	globalThis.localStorage.clear();

	const now = Date.now();
	for (let i = 0; i < 51; i++) {
		// i=0 最老, i=50 最新
		saveHistoryItem(makeItem(`uuid-${i}`, `案 ${i}`, now + i * 1000));
	}

	const items = loadHistory();
	assert.equal(items.length, 50, "应保留 50 条 (FIFO 删 1 条最旧)");
	// 最新的 uuid-50 应在第 1
	assert.equal(items[0].sessionUUID, "uuid-50", "最新应在第 1");
	// 最老的 uuid-0 应被淘汰
	assert.equal(
		items.find((x) => x.sessionUUID === "uuid-0"),
		undefined,
		"uuid-0 (最老) 应被 FIFO 淘汰",
	);
});

// 防止 TypeScript 警告 KEY 未用
void KEY;
