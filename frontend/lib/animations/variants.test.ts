// v1.0.4 PR-C3: lib/animations/variants.ts 单元测试
//
// 覆盖 6 个核心 motion variants + bubbleEnterExit:
//   - 每个 variant 都包含 initial + 目标 animate 状态
//   - speak / think / listen / search / judge / confront 都是合法 Variants 结构
//   - bubbleEnterExit 含 initial / animate / exit 三态 (AnimatePresence 必需)
//
// 不测试 framer-motion 内部行为 (那是 framer-motion 自身的测试),
// 只验证我们导出的数据结构合法 + 关键字段 (duration / repeat) 正确。

import { test } from "node:test";
import assert from "node:assert/strict";
import {
  speakVariant,
  thinkVariant,
  listenVariant,
  searchVariant,
  judgeVariant,
  confrontVariant,
  bubbleEnterExit,
} from "./variants.ts";

test("speakVariant: initial + speaking states with scale + y", () => {
  assert.ok("initial" in speakVariant);
  assert.ok("speaking" in speakVariant);
  const speaking = speakVariant.speaking as { scale: number; y: number[] };
  assert.equal(speaking.scale, 1.06);
  assert.deepEqual(speaking.y, [0, -3, 0]);
});

test("thinkVariant: pulse + rotate cycle (duration 2s)", () => {
  const thinking = thinkVariant.thinking as {
    scale: number[];
    rotate: number[];
    transition: { duration: number };
  };
  assert.deepEqual(thinking.scale, [1, 1.04, 1]);
  assert.deepEqual(thinking.rotate, [-1, 1, -1]);
  assert.equal(thinking.transition.duration, 2);
});

test("judgeVariant: 4-keyframe 敲锤 (y 0→-8→0→0)", () => {
  const judging = judgeVariant.judging as {
    y: number[];
    rotate: number[];
    transition: { times: number[]; duration: number };
  };
  assert.deepEqual(judging.y, [0, -8, 0, 0]);
  assert.deepEqual(judging.rotate, [0, -8, 8, 0]);
  assert.deepEqual(judging.transition.times, [0, 0.3, 0.6, 1]);
  assert.equal(judging.transition.duration, 0.4);
});

test("bubbleEnterExit: initial + animate + exit (AnimatePresence 必需)", () => {
  assert.ok("initial" in bubbleEnterExit);
  assert.ok("animate" in bubbleEnterExit);
  assert.ok("exit" in bubbleEnterExit, "AnimatePresence 需要 exit 状态");

  // initial 与 exit 都应包含 opacity (淡入淡出核心字段)
  const initial = bubbleEnterExit.initial as { opacity: number };
  const exit = bubbleEnterExit.exit as { opacity: number };
  assert.equal(initial.opacity, 0, "initial opacity 应为 0 (淡入起点)");
  assert.equal(exit.opacity, 0, "exit opacity 应为 0 (淡出终点)");
});

test("listenVariant + confrontVariant: 单一目标状态 (idle/confront)", () => {
  assert.ok("listening" in listenVariant);
  assert.ok("confronting" in confrontVariant);

  const listening = listenVariant.listening as { y: number[] };
  assert.deepEqual(listening.y, [0, 2, 0]);

  const confronting = confrontVariant.confronting as { x: number[] };
  assert.deepEqual(confronting.x, [0, 20, 0]);
});

test("searchVariant: 4-keyframe rotate (摇头搜索)", () => {
  const searching = searchVariant.searching as { rotate: number[] };
  assert.deepEqual(searching.rotate, [0, 15, -15, 0]);
});
