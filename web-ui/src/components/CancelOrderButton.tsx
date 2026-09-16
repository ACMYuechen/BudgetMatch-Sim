import { useEffect, useRef, useState } from 'react'
import { Alert, Button, Modal } from 'antd'
import { cancelOrder } from '@/api/mall'

export function CancelOrderButton({ orderId, disabled, onCancelled }: { orderId: string; disabled?: boolean; onCancelled: () => void }) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const requestRef = useRef<AbortController | null>(null)
  useEffect(() => () => requestRef.current?.abort(), [])

  const confirm = async () => {
    if (requestRef.current || disabled) return
    const controller = new AbortController()
    requestRef.current = controller
    setBusy(true)
    setError('')
    try {
      await cancelOrder(orderId, controller.signal)
      if (!controller.signal.aborted) { setOpen(false); onCancelled() }
    } catch (err) {
      if (!controller.signal.aborted) setError((err as Error).message)
    } finally {
      if (!controller.signal.aborted) { requestRef.current = null; setBusy(false) }
    }
  }

  return <>
    <Button danger disabled={disabled} onClick={() => { setError(''); setOpen(true) }}>取消订单</Button>
    <Modal title="确认取消订单？" open={open} onOk={confirm} onCancel={() => setOpen(false)} okText="确认取消" cancelText="保留订单" confirmLoading={busy} okButtonProps={{ danger: true }} cancelButtonProps={{ disabled: busy }} closable={!busy} maskClosable={!busy} keyboard={!busy}>
      <p>取消后无法继续支付。如需购买，需要重新下单。</p>
      <p className="commerce-note break-anywhere">订单号：{orderId}</p>
      {error && <Alert type="error" showIcon message={error} description="订单状态可能已变化，可关闭弹窗后刷新订单确认。" />}
    </Modal>
  </>
}
