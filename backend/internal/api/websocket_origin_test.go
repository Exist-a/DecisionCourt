// v0.9.3 时序坑回归测试:buildCheckOrigin 必须每次调用都重新读
// config.AppConfig.AllowedOrigins,不能闭包捕获 init 时刻的值
// (因为 upgrader 在 package init 阶段构造,config.Load() 还没跑,
// 旧实现会永远锁在 localhost:3000 fallback,生产配 ALLOWED_ORIGINS
// 无效,gorilla/websocket 返回 403)。
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/decisioncourt/backend/internal/config"
)

func TestBuildCheckOrigin_ReReadsConfigPerCall(t *testing.T) {
	// 0. 起点:AllowedOrigins 为零值
	config.AppConfig.AllowedOrigins = nil
	check := buildCheckOrigin()

	// 1. 零值状态:走 localhost:3000 fallback,生产 origin 应该被拒
	req := httptest.NewRequest("GET", "http://example.com/x", nil)
	req.Header.Set("Origin", "https://decisioncourt.cn")
	if check(req) {
		t.Fatal("AllowedOrigins 为空时,生产 origin 不应通过")
	}

	// 2. 模拟 config.Load() 在 main() 跑完后,AllowedOrigins 填好
	config.AppConfig.AllowedOrigins = []string{"https://decisioncourt.cn"}

	// 3. 关键:同一个 check 函数(已经捕获),再次调用应该读新值
	//    旧实现这里会返回 false(因为 init 时 allowedSet 锁死);
	//    新实现必须返回 true
	if !check(req) {
		t.Fatal("v0.9.3 修复失败:AllowedOrigins 设置后 check() 仍读旧值,说明闭包还在捕获 init 时刻的白名单")
	}

	// 4. 同步把 AllowedOrigins 改回去,避免影响其他测试
	config.AppConfig.AllowedOrigins = nil
}

func TestBuildCheckOrigin_EmptyOriginAlwaysAllowed(t *testing.T) {
	config.AppConfig.AllowedOrigins = []string{"https://decisioncourt.cn"}
	check := buildCheckOrigin()

	// 非浏览器调用(curl / native)无 Origin,必须通过
	req := httptest.NewRequest("GET", "http://example.com/x", nil)
	if !check(req) {
		t.Fatal("无 Origin 的请求应始终通过(非浏览器客户端)")
	}
}

func TestBuildCheckOrigin_TrimsTrailingSlash(t *testing.T) {
	config.AppConfig.AllowedOrigins = []string{"https://decisioncourt.cn/"}
	check := buildCheckOrigin()

	req := httptest.NewRequest("GET", "http://example.com/x", nil)
	req.Header.Set("Origin", "https://decisioncourt.cn/") // 带斜杠
	if !check(req) {
		t.Fatal("白名单和 Origin 末尾的 / 应该被忽略(TrimRight)")
	}

	req2 := httptest.NewRequest("GET", "http://example.com/x", nil)
	req2.Header.Set("Origin", "https://decisioncourt.cn") // 不带斜杠
	if !check(req2) {
		t.Fatal("白名单带 / Origin 不带 / 也应匹配")
	}
}

func TestBuildCheckOrigin_RejectsUnknownOrigin(t *testing.T) {
	config.AppConfig.AllowedOrigins = []string{"https://decisioncourt.cn"}
	check := buildCheckOrigin()

	req := httptest.NewRequest("GET", "http://example.com/x", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	if check(req) {
		t.Fatal("白名单外的 Origin 必须拒绝")
	}
}

// 静态断言:check 函数返回值类型必须是 func(*http.Request) bool
// (防止以后把 buildCheckOrigin 改坏导致 upgrader.CheckOrigin 类型不匹配)
var _ func(*http.Request) bool = buildCheckOrigin()
