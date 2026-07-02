package observability

import "time"

// nowUnixNano 包装 time.Now().UnixNano() 便于测试时替换（如果引入 clock 抽象）。
func nowUnixNano() int64 {
	return time.Now().UnixNano()
}