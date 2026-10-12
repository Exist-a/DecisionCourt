/**
 * R5-d 收尾（F3）：待确认证据（乐观插入的「排队中」占位）超时判定。
 *
 * 背景：占位原先只靠"证据数增加"按 FIFO 消；若提交最终失败（后端拒绝 / 网络断），
 * 占位会**永久停在「排队中」**。这里把"哪些占位已超时"抽成纯函数，便于单测覆盖
 * （组件里只做 setState 胶水）。
 */

export interface TimablePendingEvidence {
  createdAt: number;
  failed?: boolean;
}

export interface MarkTimedOutResult<T> {
  items: T[];
  changed: boolean;
}

/**
 * 把超过 timeoutMs 仍未落库、且尚未标记 failed 的占位标记为 failed。
 *
 * - 已 failed 的不再重复标记（避免每轮 tick 都产生新数组）。
 * - 无任何变化时返回**原数组引用**（React 可据此跳过重渲染）。
 * - 非正数 timeoutMs 视为禁用（不标记）。
 */
export function markTimedOut<T extends TimablePendingEvidence>(
  items: T[],
  now: number,
  timeoutMs: number,
): MarkTimedOutResult<T> {
  if (timeoutMs <= 0 || items.length === 0) {
    return { items, changed: false };
  }
  let changed = false;
  const next = items.map((it) => {
    if (!it.failed && now - it.createdAt > timeoutMs) {
      changed = true;
      return { ...it, failed: true };
    }
    return it;
  });
  return changed ? { items: next, changed } : { items, changed: false };
}
