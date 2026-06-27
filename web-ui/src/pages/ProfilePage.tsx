import { useEffect, useState } from 'react'
import { Card, Form, Input, Button, Radio, message, Spin } from 'antd'
import { SaveOutlined } from '@ant-design/icons'
import { getUserInfo, getUserProfile, updateUserProfile } from '@/api/user'
import type { UpdateUserProfileReq, UserInfo, UserProfile } from '@/types/api'

export default function ProfilePage() {
  const [form] = Form.useForm()
  const [userInfo, setUserInfo] = useState<UserInfo | null>(null)
  const [loading, setLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    setLoading(true)
    Promise.all([getUserInfo(), getUserProfile()])
      .then(([infoRes, profileRes]) => {
        setUserInfo(infoRes)
        form.setFieldsValue(profileRes)
      })
      .finally(() => setLoading(false))
  }, [form])

  const handleSubmit = async (values: UserProfile) => {
    setSubmitting(true)
    try {
      const payload: UpdateUserProfileReq = {
        real_name: values.real_name,
        school: values.school,
        major: values.major,
        grade: values.grade,
        gender: values.gender,
        expected_city: values.expected_city,
        expected_position: values.expected_position,
        self_introduction: values.self_introduction,
      }
      await updateUserProfile(payload)
      message.success('保存成功')
    } catch (err) {
      message.error((err as Error).message)
    } finally {
      setSubmitting(false)
    }
  }

  if (loading) {
    return (
      <div className="flex justify-center py-20">
        <Spin size="large" />
      </div>
    )
  }

  return (
    <div className="max-w-2xl mx-auto space-y-6">
      <h1 className="text-2xl font-bold">个人中心</h1>

      <Card title="基本信息">
        {userInfo && (
          <div className="grid grid-cols-2 gap-4 text-sm">
            <div>
              <span className="text-gray-500">用户名: </span>{userInfo.username}
            </div>
            <div>
              <span className="text-gray-500">邮箱: </span>{userInfo.email}
            </div>
            <div>
              <span className="text-gray-500">手机号: </span>{userInfo.phone || '-'}
            </div>
          </div>
        )}
      </Card>

      <Card title="编辑资料">
        <Form form={form} layout="vertical" onFinish={handleSubmit}>
          <Form.Item label="真实姓名" name="real_name">
            <Input placeholder="真实姓名" />
          </Form.Item>

          <Form.Item label="学校" name="school">
            <Input placeholder="学校" />
          </Form.Item>

          <Form.Item label="专业" name="major">
            <Input placeholder="专业" />
          </Form.Item>

          <Form.Item label="年级" name="grade">
            <Input placeholder="年级" />
          </Form.Item>

          <Form.Item label="性别" name="gender">
            <Radio.Group>
              <Radio value={0}>未知</Radio>
              <Radio value={1}>男</Radio>
              <Radio value={2}>女</Radio>
            </Radio.Group>
          </Form.Item>

          <Form.Item label="期望城市" name="expected_city">
            <Input placeholder="期望城市" />
          </Form.Item>

          <Form.Item label="期望岗位" name="expected_position">
            <Input placeholder="期望岗位" />
          </Form.Item>

          <Form.Item label="个人简介" name="self_introduction">
            <Input.TextArea rows={4} placeholder="介绍一下自己" />
          </Form.Item>

          <Form.Item>
            <Button
              type="primary"
              htmlType="submit"
              icon={<SaveOutlined />}
              loading={submitting}
            >
              保存资料
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  )
}
