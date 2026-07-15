// Package uuid 提供 infra 和 model 通用的 UUID 标识生成能力。
package uuid

import (
	"encoding/base64"
	"errors"
	"fmt"

	googleuuid "github.com/google/uuid"
)

const (
	// ShortLength 表示 128 位 UUID 移除 Base64 填充后的编码长度。
	ShortLength = 22

	// PrefixSeparator 用于分隔 model 前缀和 Short UUID。
	PrefixSeparator = "_"

	minPrefixLength = 2
	maxPrefixLength = 12
)

// ErrInvalidPrefix 表示前缀不符合 ID 生成规则。
var ErrInvalidPrefix = errors.New("invalid UUID prefix")

// Generator 表示 ID 生成函数。
type Generator func() string

// New 生成标准 UUID v4 字符串。
//
// 与 google/uuid.NewString 行为一致，无法读取操作系统安全随机源时会 panic。
func New() string {
	return googleuuid.NewString()
}

// NewShort 生成固定 22 位、可安全用于 URL 的 Base64 UUID v4 字符串。
// 该方法保留完整 128 位 UUID，仅改变编码方式，不会截断随机位。
func NewShort() string {
	id := googleuuid.New()
	return base64.RawURLEncoding.EncodeToString(id[:])
}

// NewPrefixedShort 生成使用下划线连接指定前缀的 Short UUID。
// prefix 不合法时会 panic；动态前缀应使用 NewPrefixedShortGenerator 处理错误。
func NewPrefixedShort(prefix string) string {
	return MustNewPrefixedShortGenerator(prefix)()
}

// NewPrefixedShortGenerator 校验前缀并返回可复用的 ID 生成器。
func NewPrefixedShortGenerator(prefix string) (Generator, error) {
	if err := ValidatePrefix(prefix); err != nil {
		return nil, err
	}

	return func() string {
		return prefix + PrefixSeparator + NewShort()
	}, nil
}

// MustNewPrefixedShortGenerator 返回可复用的 ID 生成器，prefix 不合法时会 panic。
// 该方法用于前缀由项目常量控制的 model 级 ID 生成器。
func MustNewPrefixedShortGenerator(prefix string) Generator {
	generator, err := NewPrefixedShortGenerator(prefix)
	if err != nil {
		panic(err)
	}
	return generator
}

// ValidatePrefix 校验前缀是否为 2 至 12 位 ASCII 字符。
// 前缀必须以小写字母开头，其余字符只能是小写字母或数字。
func ValidatePrefix(prefix string) error {
	if len(prefix) < minPrefixLength || len(prefix) > maxPrefixLength {
		return fmt.Errorf("%w: length must be between %d and %d", ErrInvalidPrefix, minPrefixLength, maxPrefixLength)
	}

	for i := range len(prefix) {
		char := prefix[i]
		if i == 0 {
			if char < 'a' || char > 'z' {
				return fmt.Errorf("%w: first character must be a lowercase ASCII letter", ErrInvalidPrefix)
			}
			continue
		}

		if (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return fmt.Errorf("%w: only lowercase ASCII letters and digits are allowed", ErrInvalidPrefix)
		}
	}

	return nil
}
