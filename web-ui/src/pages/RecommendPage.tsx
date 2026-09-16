import { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { Alert, Button, Drawer, Modal } from 'antd'
import { PlusOutlined, HistoryOutlined } from '@ant-design/icons'
import { deleteConversation, getConversationList } from '@/api/agent'
import { ConversationSidebar } from '@/components/ConversationSidebar'
import { RecommendationWorkspace } from '@/components/RecommendationWorkspace'
import { useResource } from '@/hooks/useResource'
import { isRecommendation, MAX_BUDGET_YUAN, recommendationError } from '@/utils/recommendation'
import type { AgentConversationSummary, AgentConversationTurn } from '@/types/api'

export default function RecommendPage() {
  const navigate = useNavigate()
  const { conversationId } = useParams()
  const [params] = useSearchParams()
  const location = useLocation()
  const load = useCallback((signal: AbortSignal) => getConversationList(signal), [])
  const history = useResource(load)
  const [newVersion, setNewVersion] = useState(0)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [streaming, setStreaming] = useState(false)
  const [deleting, setDeleting] = useState<AgentConversationSummary | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)
  const [deleteError, setDeleteError] = useState('')
  const deleteRequest = useRef<AbortController | null>(null)
  const currentId = useRef(conversationId)
  currentId.current = conversationId
  useEffect(() => () => deleteRequest.current?.abort(), [])

  const remove = async () => {
    if (!deleting || deleteRequest.current || streaming) return
    const controller = new AbortController()
    deleteRequest.current = controller
    setDeleteBusy(true)
    setDeleteError('')
    try {
      const result = await deleteConversation(deleting.conversation_id, controller.signal)
      if (!result.deleted) throw new Error('会话尚未删除，请重试')
      if (controller.signal.aborted) return
      if (currentId.current === deleting.conversation_id) navigate('/recommend', { replace: true })
      setDeleting(null)
      history.reload()
    } catch (err) {
      if (!controller.signal.aborted) setDeleteError(recommendationError(err))
    } finally {
      if (!controller.signal.aborted) { deleteRequest.current = null; setDeleteBusy(false) }
    }
  }

  const sidebar = <ConversationSidebar conversations={history.data || []} currentId={conversationId} loading={history.loading} error={history.error} busy={streaming || deleteBusy} onRefresh={history.reload} onSelect={(id) => { setDrawerOpen(false); navigate(`/recommend/${encodeURIComponent(id)}`) }} onDelete={(item) => { setDeleting(item); setDeleteError('') }} />
  const budget = Number(params.get('budget_cents'))
  const seed = location.state?.completedTurn as AgentConversationTurn | undefined
  const validSeed = seed && isRecommendation(seed.result) && seed.result.conversation_id === conversationId ? seed : undefined

  return <div className="commerce-page recommendation-page">
    <div className="commerce-heading"><div><span className="eyebrow">从需要出发，把预算花在喜欢的地方</span><h1>预算推荐</h1></div><div className="recommend-page-actions">
      <Button className="recommend-history-toggle" icon={<HistoryOutlined aria-hidden />} onClick={() => setDrawerOpen(true)}>会话记录</Button>
      <Button type="primary" icon={<PlusOutlined aria-hidden />} onClick={() => { setNewVersion((value) => value + 1); navigate('/recommend') }}>新对话</Button>
    </div></div>
    <div className="recommend-layout"><aside className="recommend-history-desktop" aria-label="历史会话">{sidebar}</aside>
      <RecommendationWorkspace key={`${conversationId || `new-${newVersion}`}:${params.toString()}`} conversationId={conversationId} initialQuery={conversationId ? '' : (params.get('query') || '').slice(0, 2000)} initialBudget={!conversationId && Number.isFinite(budget) && budget >= 100 && budget <= MAX_BUDGET_YUAN * 100 ? budget / 100 : undefined} seed={validSeed} onChanged={history.reload} onBusy={setStreaming} />
    </div>
    <Drawer title="我的会话" open={drawerOpen} onClose={() => setDrawerOpen(false)} width={320}>{sidebar}</Drawer>
    <Modal title="删除这个会话？" open={!!deleting} okText="确认删除" cancelText="保留会话" onOk={remove} onCancel={() => setDeleting(null)} confirmLoading={deleteBusy} okButtonProps={{ danger: true, disabled: streaming }} cancelButtonProps={{ disabled: deleteBusy }} closable={!deleteBusy} maskClosable={!deleteBusy} keyboard={!deleteBusy}>
      <p className="break-anywhere">“{deleting?.conversation_title || '未命名会话'}”及其全部轮次将被删除，无法恢复。</p>
      {deleteError && <Alert type="error" showIcon message={deleteError} />}
    </Modal>
  </div>
}
