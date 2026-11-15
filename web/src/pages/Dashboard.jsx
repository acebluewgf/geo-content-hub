import { useEffect, useState } from 'react'
import { App as AntApp, Alert, Button, Card, Col, Empty, Row, Statistic, Table, Tag, Typography } from 'antd'
import { PlusOutlined, ReloadOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { api } from '../api'
import { fmt, platformMeta, PUBLISH_STATUS } from '../utils'

export default function Dashboard() {
  const { message } = AntApp.useApp()
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const navigate = useNavigate()

  const load = async () => {
    setLoading(true)
    try {
      setData(await api('/dashboard'))
    } catch (e) { message.error(e.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    const t = setInterval(load, 30000)
    return () => clearInterval(t)
  }, [])

  const counts = data?.counts || {}
  const queue = data?.queue || {}

  const cards = [
    { title: '待发布', value: counts['status:ready'] || 0, color: '#2b5cf0' },
    { title: '排队中', value: queue.queued || 0, color: '#d48806' },
    { title: '发布中', value: queue.claimed || 0, color: '#2b5cf0' },
    { title: '已发布', value: counts['publish:published'] || 0, color: '#0f9d58' },
    { title: '失败任务', value: queue.failed || 0, color: '#d93025' },
    { title: '今日发布', value: data?.today_published || 0, color: '#0f9d58' },
  ]

  const columns = [
    {
      title: '标题',
      dataIndex: 'title',
      ellipsis: true,
      render: (v, row) => (
        <a onClick={() => navigate(`/articles/${row.uid}`)} className="link-strong">
          {v || '（无标题）'}
        </a>
      ),
    },
    {
      title: '平台',
      dataIndex: 'platform',
      width: 120,
      render: (v) => {
        if (!v) return <Tag>待定</Tag>
        const m = platformMeta(v)
        return <Tag color={m.color}>{m.label}</Tag>
      },
    },
    {
      title: '发布状态',
      dataIndex: 'publish_status',
      width: 110,
      render: (v) => {
        const s = PUBLISH_STATUS[v] || PUBLISH_STATUS.none
        return <Tag color={s.color}>{s.label}</Tag>
      },
    },
    { title: '更新时间', dataIndex: 'updated_at', width: 150, render: (v) => fmt(v) },
  ]

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            仪表盘
          </Typography.Title>
          <Typography.Text type="secondary">内容仓库与发布队列的整体状态</Typography.Text>
        </div>
        <div className="page-actions">
          <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/articles/new')}>
            新建文章
          </Button>
        </div>
      </div>

      {data && !data.scheduler ? (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 16 }}
          message="调度器心跳异常"
          description="未在配置的调度周期内检测到成功心跳，排期和租约核对可能延迟。请检查服务进程。"
        />
      ) : null}
      {counts['publish:uncertain'] > 0 && <Alert type="warning" showIcon style={{ marginBottom: 16 }} message={`${counts['publish:uncertain']} 篇文章的发布结果待核对`} action={<Button onClick={() => navigate('/queue')}>前往核对</Button>} />}

      <Row gutter={[16, 16]}>
        {cards.map((c) => (
          <Col xs={12} sm={12} md={8} xl={4} key={c.title}>
            <Card className="stat-card" bordered={false}>
              <Statistic title={c.title} value={c.value} valueStyle={{ color: c.color, fontWeight: 600 }} />
            </Card>
          </Col>
        ))}
      </Row>

      <Card
        className="panel-card"
        bordered={false}
        style={{ marginTop: 16 }}
        title="最近文章"
        extra={
          <Button type="link" icon={<ThunderboltOutlined />} onClick={() => navigate('/queue')}>
            查看发布队列
          </Button>
        }
      >
        <Table
          rowKey="uid"
          size="middle"
          loading={loading}
          columns={columns}
          dataSource={data?.recent || []}
          pagination={false}
          locale={{ emptyText: <Empty description="还没有内容，先从 API 或后台创建一篇" /> }}
        />
      </Card>
    </div>
  )
}
