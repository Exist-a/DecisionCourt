# -*- coding: utf-8 -*-
"""M10 soak 复测驱动脚本 (2026-10-12)。

用法: python scripts/soak-local-trial.py [场数]  (需 dev compose 已在跑)

场景: 同一个 backend 进程内连续跑 N 场 standard 3 轮庭审,验证:
1. 每场都推进到 verdict (无静默停滞)
2. 全程无 session 锁看门狗 WARN (无长持锁)
3. 无 hub 慢客户端 WARN

settle 判定 = 消息条数稳定 + agent_gateway FileLogger 对该 uuid 的行数稳定,
双稳定 ≥60s 且距上次 action ≥90s 才发下一个 action。
(FileLogger 只在每次 LLM 调用完成时追加,在途调用 90s 超时,所以 60s 双稳定
窗口足够安全;以防万一再加 90s 最小间隔。)
"""
import json
import subprocess
import sys
import time
import urllib.parse
import urllib.request
import http.cookiejar
import re
from pathlib import Path

BASE = "http://localhost:8180"
API = BASE + "/api/v1"
BACKEND_DIR = Path(__file__).resolve().parent.parent / "backend"
LOG_DIR = BACKEND_DIR / "logs"

N_TRIALS = int(sys.argv[1]) if len(sys.argv) > 1 else 3
POLL = 10          # 轮询间隔秒
SETTLE_NEED = 60   # 双稳定需要的秒数
MIN_ACT_GAP = 90   # 两次 action 最小间隔
PHASE_STALL = 480  # 单阶段 8 分钟无 phase 变化 = 停滞嫌疑

# 手写 cookie 管理: dc_session 带 Secure 标志且本地是 http,
# python CookieJar 会拒发 Secure cookie (浏览器对 localhost 豁免,python 没有),
# 所以手动从 Set-Cookie 头解析 dc_session / XSRF-TOKEN 并手拼 Cookie 头。
dc_session = ""
xsrf = ""

opener = urllib.request.build_opener()


def xsrf_token():
    return xsrf


def call(method, path, body=None):
    global dc_session, xsrf
    req = urllib.request.Request(API + path, method=method)
    req.add_header("Content-Type", "application/json")
    cookies = []
    if dc_session:
        cookies.append("dc_session=" + dc_session)
    if xsrf:
        cookies.append("XSRF-TOKEN=" + urllib.parse.quote(xsrf))
    if cookies:
        req.add_header("Cookie", "; ".join(cookies))
    if xsrf and method == "POST":
        req.add_header("X-XSRF-TOKEN", xsrf)
    data = json.dumps(body).encode() if body is not None else None
    try:
        r = opener.open(req, data, timeout=30)
    except urllib.error.HTTPError as e:
        r = e  # 403/4xx 时也要读 Set-Cookie 和 body
    with r:
        raw = r.read().decode("utf-8", "replace")
        for sc in r.headers.get_all("Set-Cookie") or []:
            m = re.match(r"(dc_session|XSRF-TOKEN)=([^;]+)", sc)
            if m:
                name, val = m.group(1), urllib.parse.unquote(m.group(2))
                if name == "dc_session":
                    dc_session = val
                else:
                    xsrf = val
    try:
        return json.loads(raw)
    except Exception:
        return {"code": -1, "message": raw[:200]}


def log_count_for(uuid):
    """统计 agent_gateway FileLogger 里该会话的行数 (每次完成的 LLM 调用 1 行)。"""
    n = 0
    for f in LOG_DIR.glob("agent_gateway_*.log"):
        try:
            with open(f, encoding="utf-8", errors="replace") as fh:
                for line in fh:
                    if uuid in line:
                        n += 1
        except OSError:
            pass
    return n


def docker_warns():
    """取最近 backend 日志里的锁看门狗 / 慢客户端 WARN 行数(累计)。"""
    r = subprocess.run(
        ["docker", "compose", "-f", "docker-compose.dev.yml", "logs", "backend"],
        cwd=str(BACKEND_DIR.parent), capture_output=True, text=True, encoding="utf-8", errors="replace")
    n1 = r.stdout.count("session lock held by another goroutine")
    n2 = r.stdout.count("dropping slow client")
    return n1, n2


class Trial:
    def __init__(self, idx, uuid):
        self.idx = idx
        self.uuid = uuid
        self.last_act = 0.0
        self.last_msg_count = -1
        self.last_llm_count = -1
        self.stable_since = None
        self.phase_since = time.time()
        self.last_phase = None
        self.last_round = None

    def send_action(self, action):
        r = call("POST", f"/courtrooms/{self.uuid}/actions", {"action": action})
        self.last_act = time.time()
        self.stable_since = None
        code = r.get("code")
        print(f"[T{self.idx}] action={action} code={code} msg={r.get('message','')[:80]}", flush=True)
        return code == 0


def run_trial(idx, deadline):
    title = f"soak-{time.strftime('%H%M%S')}-{idx}"
    r = call("POST", "/courtrooms", {
        "title": title, "option_a": "接受创业公司 offer",
        "option_b": "留在成形中的大厂", "mode": "standard"})
    if r.get("code") != 0:
        raise RuntimeError(f"create failed: {r}")
    uuid = r["data"]["session_uuid"] if "session_uuid" in r["data"] else r["data"].get("uuid")
    print(f"[T{idx}] created {uuid} ({title})", flush=True)

    rs = call("POST", f"/courtrooms/{uuid}/start")
    print(f"[T{idx}] start code={rs.get('code')}", flush=True)

    t = Trial(idx, uuid)
    t0 = time.time()
    verdict = None
    while time.time() < deadline:
        time.sleep(POLL)
        g = call("GET", f"/courtrooms/{uuid}")
        if g.get("code") != 0:
            print(f"[T{idx}] GET failed: {g}", flush=True)
            continue
        d = g["data"]
        phase, rnd, status = d.get("current_phase"), d.get("current_round"), d.get("status")
        msgs = call("GET", f"/courtrooms/{uuid}/messages")
        mdata = msgs.get("data") if msgs.get("code") == 0 else None
        mlist = (mdata or {}).get("messages", []) if isinstance(mdata, dict) else (mdata or [])
        mcount = len(mlist)
        llmc = log_count_for(uuid)
        now = time.time()
        if phase != t.last_phase or rnd != t.last_round:
            t.phase_since = now
            t.stable_since = None
            print(f"[T{idx}] {t.last_phase}/{t.last_round} -> {phase}/{rnd} at {time.strftime('%H:%M:%S')} "
                  f"(elapsed {now-t0:.0f}s, llm_calls={llmc})", flush=True)
            t.last_phase, t.last_round = phase, rnd

        if phase == "verdict":
            v = call("GET", f"/courtrooms/{uuid}/verdict")
            vd = (v.get("data") or {})
            verdict = {
                "elapsed_s": round(now - t0),
                "llm_calls": llmc,
                "recommendation": vd.get("recommendation"),
                "content_head": (vd.get("content") or "")[:120],
            }
            print(f"[T{idx}] ✅ VERDICT {verdict}", flush=True)
            break
        if status != "active":
            print(f"[T{idx}] ❌ status={status} phase={phase} (未到 verdict 就退出 active)", flush=True)
            break

        # settle 检测: 消息数与 LLM 调用数都稳定
        settled = (mcount == t.last_msg_count and llmc == t.last_llm_count
                   and mcount >= 0)
        if settled and t.stable_since is None:
            t.stable_since = now
            print(f"[T{idx}] 稳定窗口开启: msgs={mcount} llm_calls={llmc} "
                  f"phase={phase} round={rnd}", flush=True)
        if not settled:
            t.stable_since = None
        t.last_msg_count, t.last_llm_count = mcount, llmc

        stable_for = (now - t.stable_since) if t.stable_since else 0
        if (phase in ("opening", "cross_exam") and stable_for >= SETTLE_NEED
                and now - t.last_act >= MIN_ACT_GAP):
            action = "start_cross_exam" if phase == "opening" else "continue_cross_exam"
            t.send_action(action)

        if now - t.phase_since > PHASE_STALL:
            print(f"[T{idx}] ⚠️ STALL 嫌疑: phase={phase} round={rnd} "
                  f"{PHASE_STALL}s 无变化 (llm_calls={llmc}, msgs={mcount})", flush=True)
            t.phase_since = now  # 只报一次,继续观察是否自愈
    if verdict is None:
        print(f"[T{idx}] ❌ 未在 deadline 内到 verdict", flush=True)
    return verdict


def main():
    # 先拿 anon JWT + XSRF cookie
    import uuid as _uuid
    anon = call("POST", "/auth/anon", {"user_id": "soak-" + _uuid.uuid4().hex[:12]})
    print("auth/anon code:", anon.get("code"), flush=True)
    call("GET", "/courtrooms")  # 触发 CSRF cookie 下发
    assert xsrf_token(), "no XSRF-TOKEN cookie"

    warn0_lock, warn0_slow = docker_warns()
    print(f"baseline warns: lock={warn0_lock} slow_client={warn0_slow}", flush=True)

    deadline = time.time() + N_TRIALS * 20 * 60  # 每场最多 20 分钟
    results = []
    for i in range(1, N_TRIALS + 1):
        results.append(run_trial(i, deadline))

    warn1_lock, warn1_slow = docker_warns()
    print("\n==== SOAK SUMMARY ====", flush=True)
    for i, v in enumerate(results, 1):
        print(f"T{i}: {'OK ' + str(v) if v else '❌ FAILED (no verdict)'}", flush=True)
    print(f"lock WARNs delta: {warn1_lock - warn0_lock} | slow-client WARNs delta: {warn1_slow - warn0_slow}", flush=True)


if __name__ == "__main__":
    main()
