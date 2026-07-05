package productservice

import "budgetmatch-sim/services/rpc/mall/pb"

// 手写补充的嵌套 message 别名：goctl 仅为 RPC 出入参生成别名，
// 商品/SKU 等嵌套消息需在此显式 re-export，供下游（agent-rpc 检索/RAG 等）以
// productservice.Product / productservice.Sku 引用。此文件不会被 goctl 生成覆盖。
type (
	Product = pb.Product
	Sku     = pb.Sku
)
