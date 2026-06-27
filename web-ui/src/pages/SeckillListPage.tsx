import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Card, List, Tag, Button, Spin, Empty, Statistic } from 'antd'
import { ThunderboltOutlined, ArrowRightOutlined } from '@ant-design/icons'
import { getActivityList } from '@/api/seckill'
import { formatDateTime } from '@/utils/format'
import type { Activity } from '@/types/api'

const { Countdown } = Statistic

export default function SeckillListPage() {
  const navigate = useNavigate()
  const [activities, setActivities] = useState<Activity[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    setLoading(true)
    getActivityList({ page: 1, page_size: 100 })
      .then((res) => setActivities(res.list || []))
      .finally(() => setLoading(false))
  }, [])

  const getStatusTag = (activity: Activity) => {
    const now = new Date().getTime()
    const start = new Date(activity.start_time).getTime()
    const end = new Date(activity.end_time).getTime()

    if (now < start) return <Tag color="blue">即将开始</Tag>
    if (now > end) return <Tag color="default">已结束</Tag>
    return <Tag color="red">进行中</Tag>
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">限时秒杀</h1>

      {loading ? (
        <div className="flex justify-center py-20">
          <Spin size="large" />
        </div>
      ) : activities.length === 0 ? (
        <Empty description="暂无秒杀活动" />
      ) : (
        <List
          grid={{ gutter: 24, column: 2 }}
          dataSource={activities}
          renderItem={(activity) => {
            const now = new Date().getTime()
            const start = new Date(activity.start_time).getTime()
            const end = new Date(activity.end_time).getTime()
            const isOngoing = now >= start && now <= end
            const deadline = isOngoing ? end : start

            return (
              <List.Item>
                <Card
                  title={
                    <div className="flex items-center gap-2">
                      <ThunderboltOutlined className="text-red-500" />
                      {activity.name}
                    </div>
                  }
                  extra={getStatusTag(activity)}
                >
                  <div className="space-y-4">
                    <p className="text-gray-600">{activity.description || '暂无活动描述'}</p>
                    <div className="flex justify-between text-sm text-gray-500">
                      <span>开始: {formatDateTime(activity.start_time)}</span>
                      <span>结束: {formatDateTime(activity.end_time)}</span>
                    </div>
                    <div className="flex items-center justify-between">
                      <div>
                        <div className="text-sm text-gray-500">
                          {isOngoing ? '距离结束还剩' : '距离开始还剩'}
                        </div>
                        <Countdown value={deadline} format="HH:mm:ss" />
                      </div>
                      <Button
                        type="primary"
                        disabled={!isOngoing}
                        icon={<ArrowRightOutlined />}
                        onClick={() => navigate(`/seckill/${activity.id}`)}
                      >
                        立即参与
                      </Button>
                    </div>
                  </div>
                </Card>
              </List.Item>
            )
          }}
        />
      )}
    </div>
  )
}
