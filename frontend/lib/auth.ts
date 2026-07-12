// v0.8.3 安全(P0-1 + Q8)：客户端匿名身份管理。
//
// 流程：
//   1. 浏览器首次访问 → 检查 localStorage.dc_user_id
//   2. 不存在 → 用 crypto.randomUUID() 生成,格式: anon_<32hex>
//   3. 写 localStorage + localStorage.dc_user_id_created_at
//   4. 调用 ensureAuthToken() 拿到 JWT(set cookie + 存 localStorage.dc_token 备用)
//   5. 后续所有 API / WS 请求用 getAuthToken()
//
// 安全考量：
//   - crypto.randomUUID() 是 CSPRNG,不可枚举
//   - localStorage 不放敏感信息(只有 user_id 和 token)
//   - token 7 天过期,过期后 ensureAuthToken() 自动续期
//   - 跨标签页共享(同 origin 共享 localStorage)
//   - 跨浏览器/隐身模式 = 不同 user_id(可接受,匿名身份)

const STORAGE_KEY_UID = "dc_user_id";
const STORAGE_KEY_UID_CREATED = "dc_user_id_created_at";
const STORAGE_KEY_TOKEN = "dc_token";
const STORAGE_KEY_TOKEN_EXP = "dc_token_exp";

// isBrowser: 检测是否在浏览器环境(server-side render 时为 false)
function isBrowser(): boolean {
  return typeof window !== "undefined" && typeof localStorage !== "undefined";
}

// generateUserID: 用 crypto.randomUUID() 生成符合后端白名单 [A-Za-z0-9_.-]{1,64} 的 user_id。
//
// 格式：anon_<32hex>。例: anon_3f8a91b2e4d6c5a7b9e1f0d2c4b6a8e0
// 32 hex = 128 bit entropy,不可枚举。
export function generateUserID(): string {
  if (!isBrowser() || !crypto.randomUUID) {
    // SSR 或老浏览器：返回固定 placeholder(实际不会用,因为 ensureAuthToken 会再生成)
    return "anon_placeholder";
  }
  const uuid = crypto.randomUUID().replace(/-/g, "");
  return `anon_${uuid}`;
}

// getUserID: 拿当前 user_id；不存在则生成并存 localStorage。
export function getUserID(): string {
  if (!isBrowser()) return "";
  let uid = localStorage.getItem(STORAGE_KEY_UID);
  if (!uid) {
    uid = generateUserID();
    localStorage.setItem(STORAGE_KEY_UID, uid);
    localStorage.setItem(STORAGE_KEY_UID_CREATED, new Date().toISOString());
  }
  return uid;
}

// getUserIDCreatedAt: 返回 user_id 首次创建时间(ISO 8601),调试用。
export function getUserIDCreatedAt(): string | null {
  if (!isBrowser()) return null;
  return localStorage.getItem(STORAGE_KEY_UID_CREATED);
}

// isTokenValid: 简单检查 token 是否还有效(基于 expires_at 时间戳)。
// 服务端也会再验证一次,这是 client-side 优化,避免每次请求都白跑。
export function isTokenValid(): boolean {
  if (!isBrowser()) return false;
  const token = localStorage.getItem(STORAGE_KEY_TOKEN);
  if (!token) return false;
  const expStr = localStorage.getItem(STORAGE_KEY_TOKEN_EXP);
  if (!expStr) return false;
  const exp = parseInt(expStr, 10);
  if (Number.isNaN(exp)) return false;
  // 留 60s 缓冲(避免边界情况)
  return Date.now() < exp * 1000 - 60_000;
}

// getAuthToken: 拿 token；过期或没有则调用 ensureAuthToken()。
export function getAuthToken(): string {
  if (!isBrowser()) return "";
  if (isTokenValid()) {
    return localStorage.getItem(STORAGE_KEY_TOKEN) || "";
  }
  // token 过期 / 没有 — 同步拿一个新 token
  // 这里用阻塞方式(async/await)会让调用方复杂,所以我们用同步 fallback
  // 让真正的 token 在下一个 await tick 才好,先返回空
  void ensureAuthToken();
  return localStorage.getItem(STORAGE_KEY_TOKEN) || "";
}

// ensureAuthToken: 确保有有效 token；过期/没有就 POST /auth/anon 拿新 token。
// 返回 Promise<string>,失败返回空串。
export async function ensureAuthToken(): Promise<string> {
  if (!isBrowser()) return "";
  if (isTokenValid()) {
    return localStorage.getItem(STORAGE_KEY_TOKEN) || "";
  }

  const uid = getUserID();
  const apiUrl = process.env.NEXT_PUBLIC_API_URL || "";

  try {
    const res = await fetch(`${apiUrl}/api/v1/auth/anon`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "include", // 让后端 set cookie
      body: JSON.stringify({ user_id: uid }),
    });
    if (!res.ok) {
      // v0.10.17 silent-error-fix PR 4: 之前 console.error 后静默吞错,
      // 用户看不到任何反馈,所有 API/WS 请求都失败但不告知原因。
      // 现在用 dynamic import 避免循环依赖(auth.ts → errorBus 反向会循环),
      // toastFatal 让用户知道"匿名身份签发失败",可重试。
      void reportAuthError(`匿名身份签发失败 (HTTP ${res.status})`);
      return "";
    }
    const json = (await res.json()) as {
      code: number;
      data: { user_id: string; token: string; expires_in: number };
    };
    if (json.code !== 0) {
      void reportAuthError(`匿名身份签发被拒绝: ${json.code}`);
      return "";
    }
    const { token, expires_in } = json.data;
    const expEpoch = Math.floor(Date.now() / 1000) + expires_in;
    localStorage.setItem(STORAGE_KEY_TOKEN, token);
    localStorage.setItem(STORAGE_KEY_TOKEN_EXP, String(expEpoch));
    return token;
  } catch (err) {
    void reportAuthError(
      `匿名身份签发异常: ${err instanceof Error ? err.message : String(err)}`,
    );
    return "";
  }
}

// v0.10.17 silent-error-fix PR 4: reportAuthError 把 auth 错误通过 toast 通知用户。
// 单独抽 helper 是为了避免在 ensureAuthToken 主体里直接 dynamic import,
// 让 lint/reader 更容易看清控制流。dynamic import 是为了打破循环依赖:
// auth.ts 是 import 链上游(api.ts/websocket.ts/transport.ts 都依赖它),
// 如果 auth.ts 在模块顶层 import errorBus,会形成循环依赖。
async function reportAuthError(message: string): Promise<void> {
  try {
    const { toastFatal } = await import("./errorBus");
    toastFatal(message, { code: "AUTH_ANON_FAILED" });
  } catch {
    // errorBus 自身加载失败(理论上不会发生,极端情况如 SSR),静默兜底
  }
}

// clearAuth: 清掉本地 token(登出时调用)。user_id 保留(不重置匿名身份)。
export function clearAuth(): void {
  if (!isBrowser()) return;
  localStorage.removeItem(STORAGE_KEY_TOKEN);
  localStorage.removeItem(STORAGE_KEY_TOKEN_EXP);
}

// getWSURL: 给 WebSocket 用的 helper。带 ?token=xxx(后端 query 优先),失败时
// 浏览器自动用 cookie 兜底(同源场景)。
//
// 切换到 wss:// 如果当前页是 https:// 开头。
export function getWSURL(sessionId: string): string {
  if (!isBrowser()) return "";
  let baseUrl = process.env.NEXT_PUBLIC_WS_URL || "ws://localhost:8080";
  if (typeof window !== "undefined" && window.location.protocol === "https:") {
    baseUrl = baseUrl.replace(/^ws:/, "wss:").replace(/^http:/, "https:");
  }
  const token = getAuthToken();
  const sep = baseUrl.includes("?") ? "&" : "?";
  if (token) {
    return `${baseUrl}/ws/courtrooms/${sessionId}${sep}token=${encodeURIComponent(token)}`;
  }
  return `${baseUrl}/ws/courtrooms/${sessionId}`;
}
