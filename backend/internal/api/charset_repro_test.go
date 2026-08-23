package api

// 临时 repro: 验证 c.ShouldBindJSON 在 Windows-走-GitBash-curl 请求时
// 收到的 body 字节是否正常 (utf-8 完整保留)。
//
// 用 httptest.NewRequest 模拟 Git Bash curl "汉字测试" 的 body 字节, 走
// 同样的 gin's ShouldBindJSON 流程, 看 title 字段是否完整。

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestShouldBindJSON_ChineseBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// "汉字测试" UTF-8 = e6 b1 89 e5 ad 97 e6 b5 8b e8 af 95 (15 bytes)
	body := []byte(`{"title":"汉字测试","option_a":"A","option_b":"B"}`)

	r := gin.New()
	r.POST("/test", func(c *gin.Context) {
		var req struct {
			Title   string `json:"title"`
			OptionA string `json:"option_a"`
			OptionB string `json:"option_b"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			t.Errorf("bind failed: %v", err)
			return
		}
		t.Logf("bound title bytes: %x (len=%d)", []byte(req.Title), len(req.Title))
		t.Logf("bound title as string: %q", req.Title)
		if req.Title != "汉字测试" {
			t.Errorf("title mismatch: got %q want %q", req.Title, "汉字测试")
		}
		c.JSON(200, gin.H{"title": req.Title})
	})

	req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
	}

	// 同时验证 standard json.Unmarshal 不会出问题 (sanity check)
	var got struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("standard json.Unmarshal: %v", err)
	}
	if got.Title != "汉字测试" {
		t.Errorf("standard json unmarshal title: got %q", got.Title)
	}
}
