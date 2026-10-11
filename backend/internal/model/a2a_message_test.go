package model

import "testing"

// TestA2AMessage_TableName 钉住 A2A 消息表的真实表名。
//
// 背景（deferred-items R20）：模型长期没有 TableName()，表名由 GORM 默认复数化
// 规则生成，结果是 "a2_a_messages"（把 A2AMessage 拆成 a2_a + messages），而文档
// 一直写成 "a2a_messages" —— 照文档写 SQL 会 relation does not exist。
// 这里把表名显式化：既是生产已有表名的声明，也防止未来有人"顺手改成 a2a_messages"
// 而导致 AutoMigrate 指向不存在的表。
func TestA2AMessage_TableName(t *testing.T) {
	const want = "a2_a_messages"
	if got := (A2AMessage{}).TableName(); got != want {
		t.Errorf("A2AMessage.TableName() = %q, want %q", got, want)
	}
}
