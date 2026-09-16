import { Alert, Button, Empty, Skeleton } from 'antd'
import { DeleteOutlined } from '@ant-design/icons'
import type { AgentConversationSummary } from '@/types/api'

interface Props {
  conversations: AgentConversationSummary[]
  currentId?: string
  loading: boolean
  error?: Error
  busy: boolean
  onRefresh: () => void
  onSelect: (id: string) => void
  onDelete: (conversation: AgentConversationSummary) => void
}

export function ConversationSidebar({ conversations, currentId, loading, error, busy, onRefresh, onSelect, onDelete }: Props) {
  return <div className="conversation-sidebar">
    <div className="conversation-sidebar-heading"><h2>会话记录</h2><Button type="text" size="small" onClick={onRefresh} loading={loading}>刷新记录</Button></div>
    {loading ? <Skeleton active paragraph={{ rows: 6 }} /> : error ? <Alert type="error" showIcon message="会话列表加载失败" description={error.message} action={<Button onClick={onRefresh}>重试</Button>} />
      : conversations.length ? <ul className="conversation-list">{conversations.map((item) => <li key={item.conversation_id} className={item.conversation_id === currentId ? 'selected' : ''}>
        <button className="conversation-select" aria-current={item.conversation_id === currentId ? 'page' : undefined} onClick={() => onSelect(item.conversation_id)}>
          <strong>{item.conversation_title || '未命名会话'}</strong><span>{item.turn_count} 轮 · {new Date(item.updated_at_ms).toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' })}</span>
        </button>
        <Button type="text" size="small" disabled={busy} aria-label={`删除会话 ${item.conversation_title || '未命名会话'}`} icon={<DeleteOutlined aria-hidden />} onClick={() => onDelete(item)} />
      </li>)}</ul> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有历史会话" />}
    <p className="commerce-note">完成的推荐会保存在会话中，随时回来继续聊。</p>
  </div>
}
