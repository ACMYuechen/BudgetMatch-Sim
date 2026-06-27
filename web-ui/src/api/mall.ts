import request from './request'
import type { ListResp, Order, Product, Sku } from '@/types/api'

export function getProductList(params: {
  page: number
  page_size: number
  category_id?: string
  keyword?: string
  status?: number
}) {
  return request.get<ListResp<Product>>('/mall/products', { params })
}

export function getProductDetail(id: string) {
  return request.get<{ product: Product }>(`/mall/products/${id}`)
}

export function getSkuList(params: { product_id?: string; page?: number; page_size?: number }) {
  return request.get<ListResp<Sku>>('/mall/skus', { params })
}

export function createOrder(data: {
  sku_id: string
  quantity: number
  remark?: string
  idempotency_key: string
}) {
  return request.post<{ order_id: string; status: number }>('/mall/orders', data)
}

export function getOrderList(params: { page: number; page_size: number; status?: number }) {
  return request.get<ListResp<Order>>('/mall/orders', { params })
}

export function getOrderDetail(id: string) {
  return request.get<{ order: Order }>(`/mall/orders/${id}`)
}

export function cancelOrder(id: string) {
  return request.post<void>(`/mall/orders/${id}/cancel`, { id })
}
