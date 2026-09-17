package config

import (
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/serviceauth"
)

const indexSecretName = "agent-product-index"

// ValidateIndexAuth 允许旧部署不启用索引凭据；此时索引方法仍注册并拒绝所有请求。
func (c Config) ValidateIndexAuth() error {
	if c.IndexAuth.Secret == "" {
		return nil
	}
	return serviceauth.ValidateDedicatedSecret(c.IndexAuth.Secret, c.JwtAuth.Secret, c.ServiceAuth.Secret)
}

// RPCAuthConfig 是生产与权限回归共用的 Mall 方法级策略。
func (c Config) RPCAuthConfig() interceptor.AuthConfig {
	return interceptor.AuthConfig{
		Secret:         c.JwtAuth.Secret,
		ServiceSecret:  c.ServiceAuth.Secret,
		ServiceSecrets: map[string]string{indexSecretName: c.IndexAuth.Secret},
		ServiceMethods: map[string]interceptor.ServiceMethodPolicy{
			"/mall.OrderService/ConfirmPayment": {
				Caller: serviceauth.ServicePayment, Audience: serviceauth.ServiceMall,
			},
			"/mall.ProductIndexService/ListProductIndex": {
				Caller: serviceauth.ServiceAgent, Audience: serviceauth.ServiceMall,
				SecretName: indexSecretName, Purpose: serviceauth.PurposeProductIndexRead,
			},
		},
		AdminMethods: map[string]struct{}{
			"/mall.ProductService/CreateProduct":     {},
			"/mall.ProductService/UpdateProduct":     {},
			"/mall.ProductService/DeleteProduct":     {},
			"/mall.ProductService/CreateSku":         {},
			"/mall.ProductService/UpdateSku":         {},
			"/mall.ProductService/DeleteSku":         {},
			"/mall.OrderService/GetOrderOutboxStats": {},
			"/mall.OrderService/ListOrderOutbox":     {},
			"/mall.OrderService/GetOrderOutbox":      {},
			"/mall.OrderService/ReplayOrderOutbox":   {},
		},
	}
}
