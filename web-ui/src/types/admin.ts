import type { Order } from './api'

export interface ProductInput { name: string; content: string; image: string; providor: string; agent_comment: string; status: number; user_id?: string }
export interface SkuInput { product_id: string; name: string; specs: string; price: number; stock: number; status: number; agent_comment: string }
export interface ActivityInput { title: string; description: string; banner_url: string; start_time: number; end_time: number }
export interface SeckillSkuInput { activity_id: string; title: string; subtitle: string; pic: string; original_price: number; seckill_price: number; stock: number; sort: number; status: number; mall_sku_id?: string }
export interface AdminOrder extends Order { payment_status: number; out_trade_no: string; trade_no: string }
export interface OutboxEvent {
  id: string; aggregate_id: string; event_type: string; dedup_key: string; topic: string; tag: string; message_key: string; payload: string
  status: number; attempts: number; max_attempts: number; next_retry_at: string; locked_until: string; last_error: string
  published_at: number; created_at: string; updated_at: string
}
export interface OutboxStats { counts: { status: number; event_type: string; count: number }[]; oldest_pending_at: number }
