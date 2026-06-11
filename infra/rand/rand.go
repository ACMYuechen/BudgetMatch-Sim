package rand

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// GenerateRandomString 生成指定长度的随机字符串
func GenerateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

// GenerateRandomString32 生成长度为32的随机字符串
func GenerateRandomString32() string {
	return GenerateRandomString(32)
}

// GenCode 生成6位数字验证码
func GenCode() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return fmt.Sprintf("%06v", n.Int64())
}
