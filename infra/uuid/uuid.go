// Package uuid 提供 infra 和 model 通用的 UUID 标识生成能力。
package uuid

import (
	"encoding/base64"

	googleuuid "github.com/google/uuid"
)

// NewUUID 生成标准 UUID v4 字符串。
//
// 与 google/uuid.NewString 行为一致，无法读取操作系统安全随机源时会 panic。
func NewUUID() string {
	return googleuuid.NewString()
}

// NewShortUUID 生成固定 22 位、可安全用于 URL 的 Base64 UUID v4 字符串。
// 该方法保留完整 128 位 UUID，仅改变编码方式，不会截断随机位。
func NewShortUUID() string {
	id := googleuuid.New()
	return base64.RawURLEncoding.EncodeToString(id[:])
}

// NewPrefixedShortUUID 生成带前缀的 Short UUID。
// prefix 原样拼接、不做校验，如需分隔符（如 "usr-"）由调用方自带。
func NewPrefixedShortUUID(prefix string) string {
	return prefix + NewShortUUID()
}
