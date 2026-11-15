import { useEffect, useState } from 'react'
import {
  App as AntApp,
  Button,
  Card,
  Empty,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import { CopyOutlined, EditOutlined, KeyOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { api } from '../api'
import { fmt } from '../utils'

const { Text } = Typography

const TYPE_LABEL = { content: '内容智能体', publisher: '发布器', reviewer: '审批监控', admin: '管理员' }
const TYPE_COLOR = { content: 'blue', publisher: 'purple', reviewer: 'cyan', admin: 'gold' }

export default function Clients() {
  const { message, modal } = AntApp.useApp()
  const [items, setItems] = useState([])
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [token, setToken] = useState('')
  const [tokenOpen, setTokenOpen] = useState(false)
  const [viewToken, setViewToken] = useState('')
  const [editRow, setEditRow] = useState(null)
  const [editName, setEditName] = useState('')
  const [savingName, setSavingName] = useState(false)
  const [form] = Form.useForm()

  const load = async (p = page, ps = pageSize) => {
    setLoading(true)
    try {
      const d = await api(`/clients?page=${p}&page_size=${ps}`)
      setItems(d.items || [])
      setTotal(d.total || 0)
    } catch (e) {
      message.error(e.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load(page, pageSize)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, pageSize])

  const copyText = async (v) => {
    try {
      await navigator.clipboard.writeText(v)
      message.success('已复制')
    } catch (e) {
      message.warning('复制失败，请手动选择')
    }
  }

  const create = async (values) => {
    try {
      const d = await api('/clients', { method: 'POST', body: values })
      setCreateOpen(false)
      form.resetFields()
      setToken(d.token)
      setTokenOpen(true)
      load()
    } catch (e) {
      message.error(e.message)
    }
  }

  const rotate = (row) => {
    modal.confirm({
      title: '轮换 Token？',
      content: '旧 Token 会立即失效，正在运行的智能体需要更新配置。',
      okText: '轮换',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          const d = await api(`/clients/${row.uid}/rotate`, { method: 'POST' })
          setToken(d.token)
          setTokenOpen(true)
          load()
        } catch (e) {
          message.error(e.message)
        }
      },
    })
  }

  const toggle = async (row) => {
    try {
      await api(`/clients/${row.uid}`, { method: 'PATCH', body: { status: row.status ? 0 : 1 } })
      message.success(row.status ? '已停用' : '已启用')
      load()
    } catch (e) {
      message.error(e.message)
    }
  }

  const startEdit = (row) => {
    setEditRow(row)
    setEditName(row.name)
  }

  const saveName = async () => {
    const name = editName.trim()
    if (!name) {
      message.warning('名称不能为空')
      return
    }
    setSavingName(true)
    try {
      await api(`/clients/${editRow.uid}`, { method: 'PATCH', body: { name } })
      message.success('名称已修改')
      setEditRow(null)
      load()
    } catch (e) {
      message.error(e.message)
    } finally {
      setSavingName(false)
    }
  }

  const columns = [
    {
      title: '名称',
      dataIndex: 'name',
      width: 240,
      render: (v, row) => (
        <div>
          <div className="cell-title-main">
            {v}
            <Button
              type="text"
              size="small"
              icon={<EditOutlined />}
              onClick={() => startEdit(row)}
              style={{ marginLeft: 4 }}
            />
          </div>
          <div className="cell-title-sub mono">{row.uid}</div>
        </div>
      ),
    },
    {
      title: '类型',
      dataIndex: 'type',
      width: 120,
      render: (v, row) => (
        <Tooltip title={(row.scopes || []).join(' · ')}>
          <Tag color={TYPE_COLOR[v]}>{TYPE_LABEL[v] || v}</Tag>
        </Tooltip>
      ),
    },
    {
      title: 'Token',
      dataIndex: 'token',
      width: 300,
      render: (v) =>
        v ? (
          <Space size={4}>
            <Tooltip title={v}>
              <span className="mono token-full">
                {v.slice(0, 16)}…{v.slice(-6)}
              </span>
            </Tooltip>
            <Button size="small" type="text" icon={<CopyOutlined />} onClick={() => copyText(v)} />
            <Button size="small" type="link" onClick={() => setViewToken(v)}>
              查看
            </Button>
          </Space>
        ) : (
          <Tag>旧 Token 不可查看，请轮换</Tag>
        ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 80,
      render: (v) => (v ? <Tag color="success">启用</Tag> : <Tag>停用</Tag>),
    },
    { title: '最近使用', dataIndex: 'last_used_at', width: 170, render: (v) => fmt(v) },
    {
      title: '',
      width: 130,
      render: (_, row) => (
        <Space>
          <Button size="small" icon={<KeyOutlined />} onClick={() => rotate(row)}>
            轮换
          </Button>
          <Button size="small" type="text" onClick={() => toggle(row)}>
            {row.status ? '停用' : '启用'}
          </Button>
        </Space>
      ),
    },
  ]

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            API 客户端
          </Typography.Title>
          <Text type="secondary">Token 永久有效，可随时查看和复制；停用或轮换后旧 Token 立即失效</Text>
        </div>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={() => load()} loading={loading}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
            新建客户端
          </Button>
        </Space>
      </div>

      <Card className="panel-card" bordered={false}>
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
            showTotal: (t) => `共 ${t} 个`,
            onChange: (p, ps) => {
              setPageSize(ps)
              setPage(p)
            },
          }}
          locale={{ emptyText: <Empty description="还没有 API 客户端" /> }}
        />
      </Card>

      <Modal open={createOpen} title="新建 API 客户端" onCancel={() => setCreateOpen(false)} onOk={() => form.submit()}>
        <Form form={form} layout="vertical" onFinish={create} initialValues={{ type: 'content' }}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请填写名称' }]}>
            <Input placeholder="例如：内容智能体 / 发布器-A" maxLength={100} />
          </Form.Item>
          <Form.Item name="type" label="类型">
            <Select
              options={[
                { value: 'content', label: '内容智能体（投稿）' },
                { value: 'publisher', label: '发布器（领取与回执）' },
                { value: 'reviewer', label: '审批监控（审核状态回传）' },
                { value: 'admin', label: '管理员（全部 scopes）' },
              ]}
            />
          </Form.Item>
          <Text type="secondary">默认 scopes 按类型自动分配，Token 生成后可在列表里直接查看。</Text>
        </Form>
      </Modal>

      <Modal
        open={tokenOpen}
        title="Token 已生成"
        onCancel={() => setTokenOpen(false)}
        footer={[
          <Button key="copy" type="primary" onClick={() => copyText(token)}>
            复制 Token
          </Button>,
          <Button key="ok" onClick={() => setTokenOpen(false)}>
            关闭
          </Button>,
        ]}
      >
        <Input value={token} readOnly className="mono" />
        <p style={{ color: '#8a8f99', marginTop: 12 }}>
          该 Token 会保存在系统里，之后可以随时在列表里查看；停用或轮换后立即失效。
        </p>
        <pre className="token-hint">{`Authorization: Bearer ${token}`}</pre>
      </Modal>

      <Modal
        open={!!viewToken}
        title="Token"
        onCancel={() => setViewToken('')}
        footer={[
          <Button key="copy" type="primary" onClick={() => copyText(viewToken)}>
            复制 Token
          </Button>,
          <Button key="ok" onClick={() => setViewToken('')}>
            关闭
          </Button>,
        ]}
      >
        <Input value={viewToken} readOnly className="mono" />
        <p style={{ color: '#8a8f99', marginTop: 12 }}>该 Token 永久有效，停用或轮换后立即失效。</p>
      </Modal>

      <Modal
        open={!!editRow}
        title="修改客户端名称"
        onCancel={() => setEditRow(null)}
        onOk={saveName}
        confirmLoading={savingName}
        okText="保存"
      >
        <Input
          value={editName}
          onChange={(e) => setEditName(e.target.value)}
          onPressEnter={saveName}
          maxLength={100}
          placeholder="例如：内容智能体 / 发布器-A"
        />
        <p style={{ color: '#8a8f99', marginTop: 12 }}>仅用于后台识别，不影响 Token 鉴权。</p>
      </Modal>
    </div>
  )
}
