import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Alert, Button, Form, Modal } from 'antd'

function useOperation() {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const active = useRef<AbortController | null>(null)
  useEffect(() => () => active.current?.abort(), [])
  const run = async (action: (signal: AbortSignal) => Promise<void>, saved: () => void) => {
    if (active.current) return
    const controller = new AbortController()
    active.current = controller
    setBusy(true)
    setError('')
    try { await action(controller.signal); if (!controller.signal.aborted) saved() }
    catch (err) { if (!controller.signal.aborted) setError(err instanceof Error ? err.message : '操作失败') }
    finally { if (!controller.signal.aborted) { active.current = null; setBusy(false) } }
  }
  return { busy, error, run }
}

export function ConfirmAction({ label, description, action, onSaved, danger = false, disabled = false }: {
  label: string; description: ReactNode; action: (signal: AbortSignal) => Promise<void>; onSaved: () => void; danger?: boolean; disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const operation = useOperation()
  return <><Button danger={danger} disabled={disabled || operation.busy} onClick={() => setOpen(true)}>{label}</Button><Modal title={`确认${label}？`} open={open} okText={`确认${label}`} cancelText="暂不操作" confirmLoading={operation.busy} okButtonProps={{ danger, disabled }} cancelButtonProps={{ disabled: operation.busy }} closable={!operation.busy} maskClosable={!operation.busy} keyboard={!operation.busy} onCancel={() => setOpen(false)} onOk={() => { if (!disabled) void operation.run(action, () => { setOpen(false); onSaved() }) }}><div className="break-anywhere">{description}</div>{operation.error && <Alert type="error" showIcon message={operation.error} description="操作可能已生效，请先刷新核对，不要反复提交。" />}</Modal></>
}

export function AdminEditor<T extends object>({ title, initialValues, children, save, onClose, onSaved }: {
  title: string; initialValues: T; children: ReactNode; save: (values: T, signal: AbortSignal) => Promise<void>; onClose: () => void; onSaved: () => void
}) {
  const [form] = Form.useForm<T>()
  const operation = useOperation()
  return <Modal title={title} open width={680} okText="保存" cancelText="取消" onOk={() => form.submit()} onCancel={onClose} confirmLoading={operation.busy} okButtonProps={{ 'aria-label': '保存', disabled: operation.busy }} cancelButtonProps={{ disabled: operation.busy }} closable={!operation.busy} maskClosable={!operation.busy} keyboard={!operation.busy}><Form form={form} layout="vertical" initialValues={initialValues} disabled={operation.busy} onFinish={(values) => void operation.run((signal) => save(values, signal), onSaved)}>{children}</Form>{operation.error && <Alert type="error" showIcon message={operation.error} description="输入已保留。请求失败不一定代表未生效，请先核对列表再提交；创建接口不支持幂等重试。" />}</Modal>
}
