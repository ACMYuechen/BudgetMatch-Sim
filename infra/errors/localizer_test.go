package errors

import (
	"testing"
)

func TestNewLocalizer(t *testing.T) {
	l, err := NewLocalizer()
	if err != nil {
		t.Fatalf("NewLocalizer failed: %v", err)
	}
	if l == nil {
		t.Fatal("localizer is nil")
	}
}

func TestLocalizer_GetMessage(t *testing.T) {
	l, err := NewLocalizer()
	if err != nil {
		t.Fatalf("NewLocalizer failed: %v", err)
	}

	tests := []struct {
		msgId    string
		expected string
	}{
		{"invalid.invalid_email", "邮箱地址格式不正确"},
		{"unauthorized.invalid_token", "登录状态无效，请重新登录"},
		{"seckill.stock_not_enough", "秒杀商品库存不足"},
		{"mall.order_cannot_cancel", "当前订单不可取消"},
		{"unknown.msg_id", "unknown.msg_id"},
	}

	for _, tt := range tests {
		t.Run(tt.msgId, func(t *testing.T) {
			got := l.GetMessage(tt.msgId)
			if got != tt.expected {
				t.Errorf("GetMessage(%q) = %q, want %q", tt.msgId, got, tt.expected)
			}
		})
	}
}

func TestGetMessage_PackageLevel(t *testing.T) {
	msg := GetMessage("invalid.invalid_verify_code")
	if msg != "验证码错误或已过期" {
		t.Errorf("GetMessage = %q, want %q", msg, "验证码错误或已过期")
	}
}

func TestAppError_Message(t *testing.T) {
	err := InvalidEmail
	if got := err.Message(); got != "邮箱地址格式不正确" {
		t.Errorf("Message() = %q, want %q", got, "邮箱地址格式不正确")
	}
}

func TestAppError_Getters(t *testing.T) {
	err := InvalidEmail

	if got := err.Code(); got != int64(ECInvalidEmail) {
		t.Errorf("Code() = %d, want %d", got, ECInvalidEmail)
	}
	if got := err.MsgId(); got != "invalid.invalid_email" {
		t.Errorf("MsgId() = %q, want %q", got, "invalid.invalid_email")
	}
	if got := err.HTTPStatusCode(); got != 400 {
		t.Errorf("HTTPStatusCode() = %d, want %d", got, 400)
	}
}

func TestAppError_Nil(t *testing.T) {
	var e *AppError

	if got := e.Message(); got != "" {
		t.Errorf("nil Message() = %q, want empty", got)
	}
	if got := e.MsgId(); got != "" {
		t.Errorf("nil MsgId() = %q, want empty", got)
	}
	if got := e.Code(); got != 0 {
		t.Errorf("nil Code() = %d, want 0", got)
	}
	if got := e.HTTPStatusCode(); got != 500 {
		t.Errorf("nil HTTPStatusCode() = %d, want 500", got)
	}
}
