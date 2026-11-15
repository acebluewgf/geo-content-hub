import { useEffect, useState } from 'react'
import {
  App as AntApp,
  Alert,
  Button,
  Card,
  Descriptions,
  Empty,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { api } from '../api'
import { fmt, JOB_STATUS, platformMeta, PLATFORMS, REVIEW_STATUS } from '../utils'

const { Text } = Typography

export default function Queue() {
  const { message, modal } = AntApp.useApp()
  const [items, setItems] = useState([])
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [filters, setFilters] = useState({})
  const [detail, setDetail] = useState(null)
  const [open, setOpen] = useState(false)
  const [reconciling, setReconciling] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [reconcileForm] = Form.useForm()
  const resolution = Form.useWatch('resolution', reconcileForm)

  const load = async (p = page, ps = pageSize) => {
    setLoading(true)
    try {
      const params = new URLSearchParams({ page: String(p), page_size: String(ps) })
      Object.entries(filters).forEach(([k, v]) => v && params.set(k, v))
      const d = await api('/queue?' + params.toString())
      setItems(d.items || [])
      setTotal(d.total || 0)
    } catch (e) {
      message.error(e.message)
    } finally {
      setLoading(false)
    }
  }

  const setF = (patch) => {
    setPage(1)
    setFilters((f) => ({ ...f, ...patch }))
  }

  useEffect(() => {
    load(page, pageSize)
    const t = setInterval(() => load(page, pageSize), 15000)
    return () => clearInterval(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filters, page, pageSize])

  const openDetail = async (row) => {
    setOpen(true)
    setDetail(null)
    try {
      const d = await api('/queue/' + row.uid)
      setDetail(d.job)
    } catch (e) {
      message.error(e.message)
    }
  }

  const retry = async (uid) => {
    try {
      await api(`/queue/${uid}/retry`, { method: 'POST' })
      message.success('已重新入队')
      setOpen(false)
      load()
    } catch (e) {
      message.error(e.message)
    }
  }

  const cancel = async (uid) => {
    try {
      await api(`/queue/${uid}/cancel`, { method: 'POST' })
      message.success('已取消')
      setOpen(false)
      load()
    } catch (e) {
      message.error(e.message)
    }
  }

  const reconcile = async (values) => {
    setSubmitting(true)
    try {
      await api(`/queue/${detail.uid}/reconcile`, { method: 'POST', body: values })
      message.success(values.resolution === 'published' ? '已登记发布结果' : '已登记未发布，可按需重新入队')
      setReconciling(false)
      setOpen(false)
      load()
    } catch (e) { message.error(e.message) }
    finally { setSubmitting(false) }
  }

  const columns = [
    {
      title: '文章',
      dataIndex: 'title',
      render: (v, row) => (
        <div className="cell-title">
          <div className="cell-title-main">{v || '（无标题）'}</div>
          <div className="cell-title-sub mono">{row.uid}</div>
        </div>
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
      title: '状态',
      dataIndex: 'status',
      width: 120,
      render: (v) => {
        const s = JOB_STATUS[v] || {}
        return <Tag color={s.color}>{s.label || v}</Tag>
      },
    },
    { title: '尝试', width: 70, render: (_, r) => `${r.attempts}/${r.max_attempts}` },
    { title: '实际账号', dataIndex: 'result_account', width: 130, render: (v) => v || '—' },
    {
      title: '结果链接',
      dataIndex: 'result_url',
      width: 200,
      ellipsis: true,
      render: (v) =>
        v ? (
          <a href={v} target="_blank" rel="noreferrer">
            {v}
          </a>
        ) : (
          '—'
        ),
    },
    { title: '更新时间', dataIndex: 'updated_at', width: 150, render: (v) => fmt(v) },
  ]

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            发布队列
          </Typography.Title>
          <Text type="secondary">发布器认领任务后的所有状态，点行查看详情</Text>
        </div>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={() => load()} loading={loading}>
            刷新
          </Button>
        </Space>
      </div>

      <Card className="panel-card" bordered={false}>
        <div className="filters">
          <Select
            allowClear
            placeholder="任务状态"
            style={{ width: 160 }}
            options={Object.entries(JOB_STATUS).map(([value, s]) => ({ value, label: s.label }))}
            onChange={(v) => setF({ status: v })}
          />
          <Select
            mode="tags"
            maxCount={1}
            allowClear
            showSearch
            optionFilterProp="label"
            placeholder="平台"
            style={{ width: 170 }}
            options={Object.entries(PLATFORMS).map(([value, p]) => ({ value, label: p.label }))}
            onChange={(v) => setF({ platform: (v && v[0]) || undefined })}
          />
        </div>
        <Table
          rowKey="uid"
          size="middle"
          loading={loading}
          columns={columns}
          dataSource={items}
          pagination={{
            current: page,
            pageSize,
            total,
            showSizeChanger: true,
            pageSizeOptions: [20, 50, 100],
            showTotal: (t) => `共 ${t} 条`,
            onChange: (p, ps) => {
              setPageSize(ps)
              setPage(p)
            },
          }}
          onRow={(row) => ({ onClick: () => openDetail(row), style: { cursor: 'pointer' } })}
          locale={{ emptyText: <Empty description="队列为空" /> }}
        />
      </Card>

      <Modal
        width={820}
        open={open}
        onCancel={() => setOpen(false)}
        title="发布任务详情"
        footer={
          detail ? (
            <Space>
              {detail.status === 'uncertain' && <Button type="primary" onClick={() => { reconcileForm.resetFields(); setReconciling(true) }}>登记核对结果</Button>}
              {['failed_permanent', 'failed_retryable'].includes(detail.status) ? (
                <Button type="primary" onClick={() => retry(detail.uid)}>
                  重新入队
                </Button>
              ) : null}
              {detail.status === 'queued' ? (
                <Button danger onClick={() => cancel(detail.uid)}>
                  取消任务
                </Button>
              ) : null}
              <Button onClick={() => setOpen(false)}>关闭</Button>
            </Space>
          ) : null
        }
      >
        {detail ? (
          <div>
            {detail.status === 'uncertain' && <Alert type="warning" showIcon message="发布结果尚未确认" description="请到目标平台核实是否已经发布，再登记核对依据。核对前任务不会自动重试。" style={{ marginBottom: 16 }} />}
            {!detail.snapshot_available && <Alert type="info" message="此历史任务未保存发布内容快照，无法还原当时的正文。" style={{ marginBottom: 16 }} />}
            <Descriptions column={2} size="small" bordered>
              <Descriptions.Item label="任务">{detail.uid}</Descriptions.Item>
              <Descriptions.Item label="状态">
                <Tag color={(JOB_STATUS[detail.status] || {}).color}>
                  {(JOB_STATUS[detail.status] || {}).label || detail.status}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="文章">{detail.title}</Descriptions.Item>
              <Descriptions.Item label="平台">
                {detail.platform ? (
                  <Tag color={platformMeta(detail.platform).color}>{platformMeta(detail.platform).label}</Tag>
                ) : (
                  <Tag>待定</Tag>
                )}
              </Descriptions.Item>
              <Descriptions.Item label="尝试次数">{`${detail.attempts}/${detail.max_attempts}`}</Descriptions.Item>
              <Descriptions.Item label="账号提示">{detail.platform_account || '发布器自选'}</Descriptions.Item>
              <Descriptions.Item label="实际账号">{detail.result_account || '—'}</Descriptions.Item>
              <Descriptions.Item label="平台侧 ID">{detail.result_post_id || '—'}</Descriptions.Item>
              <Descriptions.Item label="结果链接" span={2}>
                {detail.result_url ? (
                  <a href={detail.result_url} target="_blank" rel="noreferrer">
                    {detail.result_url}
                  </a>
                ) : (
                  '—'
                )}
              </Descriptions.Item>
              <Descriptions.Item label="错误">{detail.last_error_code || '—'}</Descriptions.Item>
              <Descriptions.Item label="错误信息">{detail.last_error_message || '—'}</Descriptions.Item>
              <Descriptions.Item label="平台审核">
                <Tag color={(REVIEW_STATUS[detail.review_status] || REVIEW_STATUS.none).color}>
                  {(REVIEW_STATUS[detail.review_status] || REVIEW_STATUS.none).label}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="审核次数">{detail.review_attempts ?? 0}</Descriptions.Item>
              <Descriptions.Item label="审核备注" span={2}>
                {detail.review_note || '—'}
              </Descriptions.Item>
              <Descriptions.Item label="创建">{fmt(detail.created_at)}</Descriptions.Item>
              <Descriptions.Item label="完成">{fmt(detail.finished_at)}</Descriptions.Item>
            </Descriptions>
            {detail.snapshot_available && detail.summary ? (
              <p style={{ marginTop: 16, color: '#5b6472' }}>
                <Text type="secondary">摘要：</Text>
                {detail.summary}
              </p>
            ) : null}
            {detail.snapshot_available && <div
              className="md-preview"
              style={{ marginTop: 12 }}
              dangerouslySetInnerHTML={{ __html: detail.body_html || '' }}
            />}
            {detail.attempt_history?.length > 0 && <Table size="small" rowKey="attempt" pagination={false} dataSource={detail.attempt_history} style={{ marginTop: 16 }} columns={[
              { title: '尝试', dataIndex: 'attempt', width: 60 },
              { title: '执行者', dataIndex: 'worker_id' },
              { title: '结果', dataIndex: 'status', render: (value) => ({ reconciled_published: '已核对：已发布', reconciled_not_published: '已核对：未发布' }[value] || JOB_STATUS[value]?.label || value) },
              { title: '说明', dataIndex: 'note' },
              { title: '完成时间', dataIndex: 'finished_at', render: (value) => fmt(value) },
            ]} />}
          </div>
        ) : (
          <Empty description="加载中…" />
        )}
      </Modal>
      <Modal open={reconciling} title="登记发布核对结果" onCancel={() => !submitting && setReconciling(false)} onOk={() => reconcileForm.submit()} confirmLoading={submitting} destroyOnClose>
        <Form form={reconcileForm} layout="vertical" onFinish={reconcile} initialValues={{ resolution: 'not_published' }}>
          <Form.Item name="resolution" label="平台核对结果" rules={[{ required: true }]}><Select options={[{ value: 'not_published', label: '确认未发布' }, { value: 'published', label: '确认已经发布' }]} /></Form.Item>
          {resolution === 'published' && <>
            <Form.Item name="platform" label="实际平台" rules={[{ required: true }]}><Input maxLength={32} placeholder="例如 wechat" /></Form.Item>
            <Form.Item name="platform_account" label="实际账号" rules={[{ required: true }]}><Input maxLength={100} /></Form.Item>
            <Form.Item name="url" label="发布链接" rules={[{ required: true }, { pattern: /^https?:\/\//, message: '请输入 http(s) 链接' }]}><Input maxLength={500} /></Form.Item>
          </>}
          <Form.Item name="note" label="核对依据" rules={[{ required: true, whitespace: true, message: '请记录核对方式及结果' }]}><Input.TextArea maxLength={500} rows={3} /></Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
