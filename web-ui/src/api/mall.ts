import request from './request'
import type { ListResp, Order, Product, Sku } from '@/types/api'

export function getProductList(params: {
  page: number
  page_size: number
  keyword?: string
  status?: number
}, signal?: AbortSignal) {
  return request.get<ListResp<Product>>('/mall/products', { params, signal })
}

export function getProductDetail(id: string, signal?: AbortSignal) {
  return request.get<{ product: Product }>(`/mall/products/${encodeURIComponent(id)}`, { signal })
}

export function getSkuList(params: { product_id: string; page?: number; page_size?: number; status?: number }, signal?: AbortSignal) {
  return request.get<ListResp<Sku>>('/mall/skus', { params, signal })
}

export async function getAllProductSkus(productId: string, signal: AbortSignal): Promise<Sku[]> {
  const skus: Sku[] = []
  for (let page = 1; ; page++) {
    const result = await getSkuList({ product_id: productId, page, page_size: 100, status: 1 }, signal)
    skus.push(...(result.list || []))
    if (skus.length >= result.total || !result.list?.length) return skus
  }
}

export function createOrder(data: {
  sku_id: string
  quantity: number
  remark?: string
  idempotency_key: string
}, signal?: AbortSignal) {
  return request.post<{ order_id: string; status: number }>('/mall/orders', data, { signal })
}

export function getOrderList(params: { page: number; page_size: number; status?: number }, signal?: AbortSignal) {
  return request.get<ListResp<Order>>('/mall/orders', { params, signal })
}

export function getOrderDetail(id: string, signal?: AbortSignal) {
  return request.get<{ order: Order }>(`/mall/orders/${encodeURIComponent(id)}`, { signal })
}

export function cancelOrder(id: string, signal?: AbortSignal) {
  return request.post<void>(`/mall/orders/${encodeURIComponent(id)}/cancel`, undefined, { signal })
}

export interface PaymentSession {
  out_trade_no: string
  qr_code: string
  status: number
}

export function createPayment(id: string, signal?: AbortSignal) {
  return request.post<PaymentSession>(`/mall/orders/${encodeURIComponent(id)}/pay`, undefined, { signal })
}

export function queryPayment(id: string, signal?: AbortSignal) {
  return request.get<{ status: number; trade_no: string }>(`/mall/orders/${encodeURIComponent(id)}/pay/query`, { signal })
}
