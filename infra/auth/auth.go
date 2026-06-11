package auth

import (
	"crypto/tls"
	"net/smtp"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/crypto/bcrypt"
)

// Auth 配置
type Config struct {
	// Token 相关配置
	Secret string `json:"secret"`
	Expire int64  `json:"expire"`
	// 邮件相关配置
	Email struct {
		From     string `json:"from"`
		Password string `json:"password"`
	} `json:"email"`
}

// //////////////////////////// Token 相关 ///////////////////////////////
// 生成 Token
func GenerateToken(userId string, secret string, expire int64, role int) (string, error) {
	now := time.Now().Unix()
	claims := jwt.MapClaims{
		"user_id": userId,
		"role":    role,
		"exp":     now + expire,
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// 检查 Token 是否有效
func ValidateToken(tokenString string, secret string) (*jwt.Token, error) {
	return jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	})
}

// token 过期检查
func IsTokenExpired(tokenString string, secret string) (bool, error) {
	_, err := ValidateToken(tokenString, secret)
	if err != nil {
		if ve, ok := err.(*jwt.ValidationError); ok {
			if ve.Errors&jwt.ValidationErrorExpired != 0 {
				return true, nil
			}
		}
		return false, err
	}
	return false, nil
}

// 从 Token 中提取用户 ID
func GetUserIdFromToken(tokenString string, secret string) (string, error) {
	token, err := ValidateToken(tokenString, secret)
	if err != nil {
		return "", err
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if userId, ok := claims["user_id"].(string); ok {
			return userId, nil
		}
	}
	return "", jwt.ErrInvalidKey
}

// 从 Token 中提取用户角色
func GetUserRoleFromToken(tokenString string, secret string) (int, error) {
	token, err := ValidateToken(tokenString, secret)
	if err != nil {
		return 0, err
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if role, ok := claims["role"].(float64); ok {
			return int(role), nil
		}
	}
	return 0, jwt.ErrInvalidKey
}

// ///////////////////////////// 密码相关 ///////////////////////////////
// 加密密码
func HashPassword(password string) (string, error) {
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hashedBytes), err
}

// 验证密码
func ComparePassword(hashedPassword, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}

// / ///////////////////////////// 验证码相关 ///////////////////////////////
// 发送验证码
func SendEmail(cfg Config, to string, code string) error {
	from := cfg.Email.From
	password := cfg.Email.Password

	smtpHost := "smtp.qq.com"
	smtpPort := "465"

	subject := "登录验证码"
	body := `<h3>您的验证码是：` + code + `</h3><p>5分钟内有效</p>`

	msg := []byte(
		"To: " + to + "\r\n" +
			"From: " + from + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/html; charset=UTF-8\r\n\r\n" +
			body)

	// 配置 TLS 连接
	tlsConfig := &tls.Config{ServerName: smtpHost}
	// 连接 SMTP 服务器
	conn, err := tls.Dial("tcp", smtpHost+":"+smtpPort, tlsConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	// 创建 SMTP 客户端
	client, err := smtp.NewClient(conn, smtpHost)
	if err != nil {
		return err
	}
	// 认证并发送邮件
	auth := smtp.PlainAuth("", from, password, smtpHost)
	if err = client.Auth(auth); err != nil {
		return err
	}
	// 设置发件人和收件人
	if err = client.Mail(from); err != nil {
		return err
	}
	if err = client.Rcpt(to); err != nil {
		return err
	}

	// 发送邮件内容
	w, err := client.Data()
	if err != nil {
		return err
	}
	_, err = w.Write(msg)
	if err != nil {
		return err
	}
	w.Close()
	client.Quit()
	// 发送成功
	return nil
}
