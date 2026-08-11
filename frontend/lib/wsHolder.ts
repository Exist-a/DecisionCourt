// D2-L2 (v0.10.x D2 silent-error-fix 收尾): 模块级 wsRef, 让 store
// (非 React 上下文) 能在 toastAction.onClick 里调 ws.send。
//
// 之前 wsRef 是 CourtroomScene 的 React useRef, store 拿不到。
// 这里提取 module-level, CourtroomScene 在 setWs 时调 setGlobalWsRef,
// store 在 retry onClick 调 globalWsRef()?.send({action: ...})。
//
// 设计取舍:
//   - module-level vs 全局事件总线: module-level 简单直接, 不需要事件注册/解绑
//   - 单例 vs 弱引用: 单例够用 (项目只 1 个庭审页)
//   - React useRef vs module-level: 必须 module-level 才能在 store (非 React 上下文) 用

import type { UserActionRequest } from "@/types";

interface WsLike {
	send: (action: UserActionRequest) => void;
}

let globalWs: WsLike | null = null;

export function setGlobalWsRef(ws: WsLike | null): void {
	globalWs = ws;
}

export function globalWsRef(): WsLike | null {
	return globalWs;
}
