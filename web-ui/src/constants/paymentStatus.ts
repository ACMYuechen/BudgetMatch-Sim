// 支付流水与商城订单不是同一状态枚举。
// services/rpc/payment/model/payments/payments_model_gen.go
export const PaymentStatus = { PENDING: 0, SUCCESS: 1, CLOSED: 2 } as const
