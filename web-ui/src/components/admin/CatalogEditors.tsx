import { Alert, Form, Input, InputNumber, Select } from 'antd'
import { saveActivity, saveProduct, saveSeckillSku, saveSku } from '@/api/admin'
import { AdminEditor } from './AdminActions'
import { localDateInput } from '@/utils/admin'
import type { Activity, Product, SeckillSku, Sku } from '@/types/api'
import type { ActivityInput, ProductInput, SeckillSkuInput, SkuInput } from '@/types/admin'

function TextField({ name, label, required = false, previous = '', max = 512, long = false }: { name: string; label: string; required?: boolean; previous?: string; max?: number; long?: boolean }) {
  return <Form.Item name={name} label={label} rules={[{ required: required || !!previous, whitespace: true, message: previous ? '现有接口不支持清空此字段，请保留或替换内容' : `请填写${label}` }, { max, message: `最多 ${max} 个字符` }]}>{long ? <Input.TextArea maxLength={max} autoSize={{ minRows: 2, maxRows: 6 }} /> : <Input maxLength={max} />}</Form.Item>
}
function NumberField({ name, label, min = 0, max = 2147483647 }: { name: string; label: string; min?: number; max?: number }) {
  return <Form.Item name={name} label={label} rules={[{ required: true, type: 'integer', min, max, message: `请输入 ${min} 至 ${max} 的整数` }]}><InputNumber min={min} max={max} precision={0} /></Form.Item>
}
const saleOptions = [{ value: 1, label: '上架' }, { value: 0, label: '下架' }]
interface EditorProps { onClose: () => void; onSaved: () => void }

export function ProductEditor({ product, ...props }: EditorProps & { product?: Product }) {
  const values: ProductInput = { name: product?.name || '', content: product?.content || '', image: product?.image || '', providor: product?.providor || '', agent_comment: product?.agent_comment || '', status: product?.status ?? 1, user_id: product?.user_id || '' }
  return <AdminEditor title={product ? '编辑商品' : '新建商品'} initialValues={values} save={(data, signal) => saveProduct(product?.id, data, signal)} {...props}>
    <TextField name="name" label="商品名称" required max={128} />{!product && <TextField name="user_id" label="归属用户 ID" required max={64} />}
    <TextField name="providor" label="供应商" previous={product?.providor} max={128} /><TextField name="image" label="图片地址" previous={product?.image} /><TextField name="content" label="商品描述" long max={5000} previous={product?.content} /><TextField name="agent_comment" label="推荐备注" long previous={product?.agent_comment} />
    {product ? <Form.Item name="status" label="商品状态" rules={[{ required: true }]}><Select options={saleOptions} /></Form.Item> : <Alert type="info" message="新商品由后端默认上架，保存后可进入详情调整状态。" />}
  </AdminEditor>
}

export function SkuEditor({ sku, productId, ...props }: EditorProps & { sku?: Sku; productId: string }) {
  const values: SkuInput = { product_id: productId, name: sku?.name || '', specs: sku?.specs || '', price: sku?.price || 1, stock: sku?.stock ?? 1, status: sku?.status ?? 1, agent_comment: sku?.agent_comment || '' }
  return <AdminEditor title={sku ? '编辑商城规格' : '新增商城规格'} initialValues={values} save={(data, signal) => saveSku(sku?.id, { ...data, product_id: productId }, signal)} {...props}>
    <TextField name="name" label="规格名称" required max={128} /><TextField name="specs" label="规格描述 / JSON" previous={sku?.specs} long /><div className="admin-form-grid"><NumberField name="price" label="价格（分）" min={1} max={100000000000} /><NumberField name="stock" label="库存数量" min={sku ? 0 : 1} /></div><TextField name="agent_comment" label="推荐备注" previous={sku?.agent_comment} />
    {sku ? <Form.Item label="规格状态" name="status" rules={[{ required: true }]}><Select options={saleOptions} /></Form.Item> : <Alert type="info" message="新规格默认上架；创建接口要求初始库存至少为 1，编辑时可改为 0。" />}
    <p className="commerce-note">价格以分提交。库存为绝对数量，保存前请核对当前库存，避免覆盖并发变化。</p>
  </AdminEditor>
}

type ActivityForm = Omit<ActivityInput, 'start_time' | 'end_time'> & { start_time: string; end_time: string }
export function ActivityEditor({ activity, ...props }: EditorProps & { activity?: Activity }) {
  const values: ActivityForm = { title: activity?.title || '', description: activity?.description || '', banner_url: activity?.banner_url || '', start_time: localDateInput(activity?.start_time || Date.now() + 3600000), end_time: localDateInput(activity?.end_time || Date.now() + 7200000) }
  return <AdminEditor title={activity ? '编辑活动' : '新建活动'} initialValues={values} save={(data, signal) => saveActivity(activity?.id, { ...data, start_time: new Date(data.start_time).getTime(), end_time: new Date(data.end_time).getTime() }, signal)} {...props}>
    <TextField name="title" label="活动标题" required max={128} /><TextField name="description" label="活动描述" long previous={activity?.description} /><TextField name="banner_url" label="活动图片地址" previous={activity?.banner_url} />
    <Form.Item name="start_time" label="开始时间（本地时区）" rules={[{ required: true, message: '请选择开始时间' }, { validator: (_, value) => Number.isFinite(new Date(value).getTime()) ? Promise.resolve() : Promise.reject(new Error('时间格式不正确')) }]}><Input type="datetime-local" /></Form.Item>
    <Form.Item name="end_time" label="结束时间（本地时区）" dependencies={['start_time']} rules={[{ required: true, message: '请选择结束时间' }, ({ getFieldValue }) => ({ validator: (_, value) => new Date(value).getTime() > new Date(getFieldValue('start_time')).getTime() ? Promise.resolve() : Promise.reject(new Error('结束时间必须晚于开始时间')) })]}><Input type="datetime-local" /></Form.Item>
    <p className="commerce-note">保存时转换为毫秒时间戳。新活动默认下线，配置商品后再手动预热、上线。</p>
  </AdminEditor>
}

export function SeckillSkuEditor({ sku, activityId, ...props }: EditorProps & { sku?: SeckillSku; activityId: string }) {
  const values: SeckillSkuInput = { activity_id: activityId, title: sku?.title || '', subtitle: sku?.subtitle || '', pic: sku?.pic || '', original_price: sku?.original_price || 0, seckill_price: sku?.seckill_price || 1, stock: sku?.stock || 0, sort: sku?.sort || 0, status: sku?.status ?? 1, mall_sku_id: '' }
  return <AdminEditor title={sku ? '编辑秒杀商品' : '新增秒杀商品'} initialValues={values} save={(data, signal) => saveSeckillSku(sku?.id, { ...data, activity_id: activityId }, signal)} {...props}>
    <TextField name="title" label="秒杀商品标题" required max={128} /><TextField name="subtitle" label="副标题" max={256} previous={sku?.subtitle} /><TextField name="pic" label="商品图片地址" previous={sku?.pic} />{!sku && <TextField name="mall_sku_id" label="关联商城 SKU ID（可选）" max={36} />}
    <div className="admin-form-grid"><NumberField name="original_price" label="原价（分）" min={sku?.original_price ? 1 : 0} max={100000000000} /><NumberField name="seckill_price" label="秒杀价（分）" min={1} max={100000000000} /><NumberField name="stock" label="总库存" min={sku?.sold || 0} /><NumberField name="sort" label="排序值" /></div>
    {sku && <Form.Item label="秒杀商品状态" name="status" rules={[{ required: true }]}><Select options={[...saleOptions, { value: 2, label: '禁用' }]} /></Form.Item>}
    <Alert type="warning" message="库存为活动总库存，不能小于已售数量。修改后需在合适时机重新预热；预热会重置 Redis 库存。" />
  </AdminEditor>
}
