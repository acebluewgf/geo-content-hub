import { useEffect, useState } from 'react'
import {
  App as AntApp,
  Button,
  Card,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Space,
  Tag,
  Typography,
} from 'antd'
import { SaveOutlined } from '@ant-design/icons'
import { api } from '../api'
import { fmt } from '../utils'

const { Text } = Typography

const FIELDS = [
  { key: 'review.first_delay_seconds', min: 30, max: 86400, label: '首次审核等待（秒）', type: 'int' },
  { key: 'review.interval_seconds', min: 300, max: 604800, label: '审核复查间隔（秒）', type: 'int' },
  { key: 'publish.lease_seconds', min: 300, max: 3600, label: '租约秒数', type: 'int', help: '发布器认领任务后的租约时长' },
  { key: 'publish.max_attempts', min: 1, max: 10, label: '最大尝试次数', type: 'int', help: '确认未发布的失败才按此上限重试；未知结果需先核对' },
  { key: 'publish.tick_interval', min: 10, max: 600, label: '调度间隔（秒）', type: 'int', help: '服务内置调度器的 tick 周期' },
  { key: 'api.rate_per_min', min: 1, max: 10000, label: 'API 每分钟限额', type: 'int', help: '每个客户端默认限流' },
  { key: 'media.max_upload_mb', min: 1, max: 25, label: '图片上限（MB）', type: 'int', help: '单张图片最大体积' },
]

export default function Settings() {
  const { message } = AntApp.useApp()
  const [form] = Form.useForm()
  const [saving, setSaving] = useState(false)
  const [health, setHealth] = useState(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    Promise.all([
      api('/settings'),
      fetch('/healthz')
        .then((r) => r.json())
        .catch(() => null),
    ])
      .then(([s, h]) => {
        form.setFieldsValue(s.settings || {})
        setHealth(h)
      })
      .catch((e) => message.error(e.message))
      .finally(() => setLoading(false))
  }, [])

  const save = async (values) => {
    setSaving(true)
    try {
      await api('/settings', { method: 'PUT', body: values })
      message.success('设置已保存')
    } catch (e) {
      message.error(e.message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            设置
          </Typography.Title>
          <Text type="secondary">发布节奏、限流与系统信息</Text>
        </div>
      </div>

      <Card className="panel-card" bordered={false} title="系统健康" style={{ marginBottom: 16 }}>
        {health ? (
          <Descriptions column={2} size="small">
            <Descriptions.Item label="状态">
              <Tag color={health.status === 'ok' ? 'success' : 'error'}>{health.status}</Tag>
            </Descriptions.Item>
            <Descriptions.Item label="数据库">
              <Tag color={health.db ? 'success' : 'error'}>{health.db ? '正常' : '异常'}</Tag>
            </Descriptions.Item>
            <Descriptions.Item label="调度器">
              <Tag color={health.scheduler ? 'success' : 'error'}>{health.scheduler ? '心跳正常' : '心跳超时'}</Tag>
            </Descriptions.Item>
            <Descriptions.Item label="时间">{fmt(health.time)}</Descriptions.Item>
            <Descriptions.Item label="版本">{health.version}</Descriptions.Item>
          </Descriptions>
        ) : (
          <Text type="secondary">加载中…</Text>
        )}
      </Card>

      <Card className="panel-card" bordered={false} title="发布与限流">
        <Form form={form} layout="vertical" onFinish={save} disabled={loading}>
          <div className="settings-grid">
            {FIELDS.map((f) => (
              <Form.Item key={f.key} name={f.key} label={f.label} extra={f.help}>
                {f.type === 'int' ? <InputNumber min={f.min} max={f.max} precision={0} style={{ width: '100%' }} /> : <Input />}
              </Form.Item>
            ))}
          </div>
          <Space>
            <Button type="primary" htmlType="submit" icon={<SaveOutlined />} loading={saving}>
              保存设置
            </Button>
          </Space>
        </Form>
      </Card>
    </div>
  )
}
