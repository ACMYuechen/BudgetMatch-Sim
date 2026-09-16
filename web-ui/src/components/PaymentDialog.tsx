import { useEffect, useState } from 'react'
import { Alert, Button, Modal, QRCode } from 'antd'
import { queryPayment, type PaymentSession } from '@/api/mall'
import { PaymentStatus } from '@/constants/paymentStatus'

interface Props {
  orderId: string
  payment: PaymentSession
  onClose: () => void
  onSettled: (status: number) => void
}

// 弹窗卸载即停止轮询；上一轮结束后才启动下一轮，不并发堆积请求。
export function PaymentDialog({ orderId, payment, onClose, onSettled }: Props) {
  const [status, setStatus] = useState(payment.status)
  const [error, setError] = useState('')
  const [paused, setPaused] = useState(false)
  const [checking, setChecking] = useState(false)
  const [revision, setRevision] = useState(0)
  const terminal = status === PaymentStatus.SUCCESS || status === PaymentStatus.CLOSED

  useEffect(() => {
    if (payment.status !== PaymentStatus.PENDING) {
      if (payment.status === PaymentStatus.SUCCESS || payment.status === PaymentStatus.CLOSED) onSettled(payment.status)
      else setError('支付服务返回了未知状态，请关闭后刷新订单确认，勿重复付款。')
      return
    }
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    let attempts = 0
    setError('')
    setPaused(false)
    const check = async () => {
      setChecking(true)
      try {
        const result = await queryPayment(orderId, controller.signal)
        if (controller.signal.aborted) return
        setStatus(result.status)
        if (result.status === PaymentStatus.SUCCESS || result.status === PaymentStatus.CLOSED) {
          onSettled(result.status)
          return
        }
        if (result.status !== PaymentStatus.PENDING) throw new Error('无法识别支付状态，请先刷新订单确认，勿重复付款')
        attempts++
        if (attempts >= 40) setPaused(true)
        else timer = setTimeout(check, 3000)
      } catch (err) {
        if (!controller.signal.aborted) setError((err as Error).message)
      } finally {
        if (!controller.signal.aborted) setChecking(false)
      }
    }
    // 让 StrictMode 的首次清理先完成，避免在开发环境重复查询。
    timer = setTimeout(check, 0)
    return () => { controller.abort(); clearTimeout(timer) }
  }, [orderId, payment.status, revision, onSettled])

  return <Modal title="支付宝扫码支付" open onCancel={onClose} centered width={420} footer={<Button onClick={onClose}>{terminal ? '完成' : '关闭'}</Button>}>
    <div className="payment-content">
      {status === PaymentStatus.SUCCESS ? <Alert type="success" showIcon message="支付成功" description="已查询到支付成功，订单信息正在刷新。请勿重复付款。" />
        : status === PaymentStatus.CLOSED ? <Alert type="warning" showIcon message="支付已关闭" description="当前付款码已不可用，请关闭弹窗并刷新订单确认。" />
          : status !== PaymentStatus.PENDING ? <Alert type="error" showIcon message="无法识别支付状态" description="请关闭弹窗后刷新订单确认，勿重复付款。" /> : <>
            {payment.qr_code ? <QRCode value={payment.qr_code} size={208} /> : <Alert type="warning" message="未获取到付款码" description="请先确认支付状态，勿重复付款。" />}
            <p>请使用支付宝扫描二维码支付</p>
            {error && <Alert type="error" showIcon message="支付状态查询失败" description={error} />}
            {paused && <Alert type="info" showIcon message="自动查询已暂停" description="尚未确认支付结果，可手动查询。暂停不代表支付失败。" />}
            {!error && !paused && <p className="commerce-note" role="status">正在等待支付结果…</p>}
            <Button loading={checking} onClick={() => setRevision((value) => value + 1)}>我已支付，查询结果</Button>
          </>}
      <p className="commerce-note break-anywhere">支付单号：{payment.out_trade_no || '待确认'}</p>
      {!terminal && <p className="commerce-note">关闭弹窗只停止查询，不会取消订单或支付。</p>}
    </div>
  </Modal>
}
