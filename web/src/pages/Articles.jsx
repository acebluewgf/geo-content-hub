import { useEffect, useRef, useState } from 'react'
import {
  App as AntApp,
  Button,
  Card,
  DatePicker,
  Descriptions,
  Drawer,
  Dropdown,
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
import { CopyOutlined, EditOutlined, MoreOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import { useNavigate } from 'react-router-dom'
import { api } from '../api'
import { ARTICLE_STATUS, fmt, platformMeta, PLATFORMS, PUBLISH_STATUS, REVIEW_STATUS } from '../utils'

const { Text } = Typography

const FILTER_DIMS = ['status', 'publish_status', 'review_status', 'platform']

function StatPill({ active, label, count, onClick }) {
  return (
    <button type="button" className={'stat-pill' + (active ? ' active' : '')} onClick={onClick}>
      {label}
      <b>{count ?? 0}</b>
    </button>
  )
}

export default function Articles() {
  const { message, modal } = AntApp.useApp()
  const navigate = useNavigate()
  const [items, setItems] = useState([])
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [total, setTotal] = useState(0)
  const [stats, setStats] = useState(null)
  const [statsAt, setStatsAt] = useState('')
  const [loading, setLoading] = useState(false)
  const [filters, setFilters] = useState({})
  const [detail, setDetail] = useState(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [modalType, setModalType] = useState(null)
  const [modalRow, setModalRow] = useState(null)
  const [form] = Form.useForm()

  const seq = useRef(0)
  const load = async (p = page, ps = pageSize) => {
    const my = ++seq.current
    setLoading(true)
    try {
      const params = new URLSearchParams({ page: String(p), page_size: String(ps) })
      Object.entries(filters).forEach(([k, v]) => {
        if (v) params.set(k, v)
      })
      const data = await api('/articles?' + params.toString())
      if (my !== seq.current) return
      setItems(data.items || [])
      setTotal(data.total || 0)
    } catch (e) {
      if (my === seq.current) message.error(e.message)
    } finally {
      if (my === seq.current) setLoading(false)
    }
  }

  // 统计各项是全量计数；为保证「点哪个数字，列表就是几篇」，快捷筛选一次只保留一个维度。
  const pickFilter = (dim, value) => {
    setPage(1)
    setFilters((f) => {
      const next = { ...f, [dim]: value }
      FILTER_DIMS.forEach((d) => {
        if (d !== dim) delete next[d]
      })
      return next
    })
  }

  const clearFilters = () => {
    setPage(1)
    setFilters((f) => {
      const next = { ...f }
      FILTER_DIMS.forEach((d) => delete next[d])
      return next
    })
  }

  const setQuery = (v) => {
    setPage(1)
    setFilters((f) => ({ ...f, q: v }))
  }

  const loadStats = async () => {
    try {
      const params = new URLSearchParams()
      ;['q', 'from', 'to', 'language'].forEach((k) => {
        if (filters[k]) params.set(k, filters[k])
      })
      const data = await api('/articles/stats?' + params.toString())
      setStats(data)
      setStatsAt(dayjs().format('HH:mm:ss'))
    } catch (e) {
      // 统计失败不阻塞列表
    }
  }

  const reloadAll = () => {
    load()
    loadStats()
  }

  useEffect(() => {
    loadStats()
    const timer = setInterval(loadStats, 15000)
    return () => clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filters.q, filters.from, filters.to, filters.language])

  useEffect(() => {
    load(page, pageSize)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filters, page, pageSize])

  const openDetail = async (row) => {
    setDrawerOpen(true)
    setDetail(null)
    try {
      const data = await api('/articles/' + row.uid)
      setDetail(data.article)
    } catch (e) {
      message.error(e.message)
    }
  }

  const act = async (path, body, okText) => {
    try {
      await api(path, { method: 'POST', body })
      message.success(okText)
      setDrawerOpen(false)
      reloadAll()
    } catch (e) {
      message.error(e.message)
    }
  }

  const openModal = (type, row) => {
    setModalType(type)
    setModalRow(row)
    form.resetFields()
    if (type === 'schedule' && row.scheduled_at) {
      form.setFieldsValue({ scheduled_at: dayjs(row.scheduled_at) })
    }
    if (type === 'manual' && row.platform_account) {
      form.setFieldsValue({ platform_account: row.platform_account })
    }
  }

  const submitModal = async (values) => {
    const uid = modalRow.uid
    try {
      if (modalType === 'schedule') {
        await api(`/articles/${uid}/schedule`, {
          method: 'POST',
          body: {
            scheduled_at: values.scheduled_at.toISOString(),
            ...(values.platform_account ? { platform_account: values.platform_account } : {}),
          },
        })
        message.success('已排期')
      } else {
        await api(`/articles/${uid}/manual-publish`, {
          method: 'POST',
          body: {
            platform: values.platform,
            platform_account: values.platform_account,
            url: values.url,
            post_id: values.post_id || '',
          },
        })
        message.success('已标记为手动发布')
      }
      setModalType(null)
      setDrawerOpen(false)
      reloadAll()
    } catch (e) {
      message.error(e.message)
    }
  }

  const moreItems = (row) => {
    const items = [
      { key: 'edit', label: '编辑', onClick: () => navigate(`/articles/${row.uid}`) },
      {
        key: 'schedule',
        label: '设置排期',
        disabled: row.status !== 'ready' || row.publish_status === 'published',
        onClick: () => openModal('schedule', row),
      },
      {
        key: 'manual',
        label: '标记已发布',
        disabled: row.publish_status === 'published',
        onClick: () => openModal('manual', row),
      },
      {
        key: 'cancel',
        label: '取消发布',
        disabled: !['scheduled', 'queued'].includes(row.publish_status),
        onClick: () =>
          modal.confirm({
            title: '取消这次发布？',
            content: '任务会被取消，文章回到未发布状态，之后可以重新排期。',
            okText: '取消发布',
            okButtonProps: { danger: true },
            onOk: () => act(`/articles/${row.uid}/cancel`, {}, '已取消'),
          }),
      },
      { type: 'divider' },
      {
        key: 'duplicate',
        label: '复制为新文章',
        onClick: () =>
          api(`/articles/${row.uid}/duplicate`, { method: 'POST' })
            .then((d) => {
              message.success('已复制')
              load()
              navigate(`/articles/${d.article.uid}`)
            })
            .catch((e) => message.error(e.message)),
      },
      {
        key: 'archive',
        label: '归档',
        danger: true,
        onClick: () =>
          modal.confirm({
            title: '归档这篇文章？',
            content: '归档后不再出现在默认列表，也不会进入发布队列。',
            okButtonProps: { danger: true },
            onOk: () => act(`/articles/${row.uid}/archive`, {}, '已归档'),
          }),
      },
    ]
    return { items }
  }

  const columns = [
    {
      title: '封面',
      dataIndex: 'cover',
      width: 60,
      render: (v) => (
        <div className="cover-cell">
          {v?.url ? <img src={v.url} alt="封面" loading="lazy" /> : <span className="cover-cell-empty">无图</span>}
        </div>
      ),
    },
    {
      title: '标题',
      dataIndex: 'title',
      width: 280,
      ellipsis: true,
      render: (v, row) => (
        <div className="cell-title">
          <div className="cell-title-main">{v || '（无标题）'}</div>
          <div className="cell-title-sub">{row.summary || row.topic || '—'}</div>
        </div>
      ),
    },
    {
      title: '平台',
      dataIndex: 'platform',
      width: 96,
      render: (v) => {
        if (!v) return <Tag>待定</Tag>
        const m = platformMeta(v)
        return <Tag color={m.color}>{m.label}</Tag>
      },
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 80,
      render: (v) => {
        const s = ARTICLE_STATUS[v] || {}
        return <Tag color={s.color}>{s.label || v}</Tag>
      },
    },
    {
      title: '发布状态',
      dataIndex: 'publish_status',
      width: 96,
      render: (v) => {
        const s = PUBLISH_STATUS[v] || PUBLISH_STATUS.none
        return <Tag color={s.color}>{s.label}</Tag>
      },
    },
    {
      title: '平台审核',
      dataIndex: 'review_status',
      width: 84,
      render: (v) => {
        const s = REVIEW_STATUS[v] || REVIEW_STATUS.none
        return <Tag color={s.color}>{s.label}</Tag>
      },
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      width: 104,
      render: (v) => (
        <span className="cell-time" title={fmt(v)}>
          {dayjs(v).format('MM-DD HH:mm')}
        </span>
      ),
    },
    {
      title: '',
      width: 72,
      render: (_, row) => (
        <Space onClick={(e) => e.stopPropagation()}>
          <Button size="small" type="text" icon={<EditOutlined />} onClick={() => navigate(`/articles/${row.uid}`)} />
          <Dropdown menu={moreItems(row)} trigger={['click']}>
            <Button size="small" type="text" icon={<MoreOutlined />} />
          </Dropdown>
        </Space>
      ),
    },
  ]

  const activeChips = []
  if (filters.status) activeChips.push('文章状态：' + ((ARTICLE_STATUS[filters.status] || {}).label || filters.status))
  if (filters.publish_status)
    activeChips.push('发布状态：' + ((PUBLISH_STATUS[filters.publish_status] || {}).label || filters.publish_status))
  if (filters.review_status)
    activeChips.push('平台审核：' + ((REVIEW_STATUS[filters.review_status] || {}).label || filters.review_status))
  if (filters.platform) activeChips.push('平台：' + platformMeta(filters.platform).label)

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            文章
          </Typography.Title>
          <Typography.Text type="secondary">智能体投稿与人工创建的内容都在这里</Typography.Text>
        </div>
        <div className="page-actions">
          <Button icon={<ReloadOutlined />} onClick={reloadAll} loading={loading}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/articles/new')}>
            新建文章
          </Button>
        </div>
      </div>

      <Card className="stats-card" bordered={false}>
        <div className="stats-head">
          <span className="stats-title">数据概览</span>
          <span className="stats-updated">更新于 {statsAt || '—'}</span>
        </div>
        <div className="stat-groups">
          <div className="stat-group">
            <span className="stat-group-label">文章状态</span>
            <StatPill
              active={!filters.status}
              label="全部"
              count={(stats || {}).total}
              onClick={() => pickFilter('status', undefined)}
            />
            <StatPill
              active={filters.status === 'draft'}
              label="草稿"
              count={((stats || {}).status || {}).draft}
              onClick={() => pickFilter('status', filters.status === 'draft' ? undefined : 'draft')}
            />
            <StatPill
              active={filters.status === 'ready'}
              label="待发布"
              count={((stats || {}).status || {}).ready}
              onClick={() => pickFilter('status', filters.status === 'ready' ? undefined : 'ready')}
            />
            <StatPill
              active={filters.status === 'archived'}
              label="归档"
              count={((stats || {}).status || {}).archived}
              onClick={() => pickFilter('status', filters.status === 'archived' ? undefined : 'archived')}
            />
          </div>

          <div className="stat-group">
            <span className="stat-group-label">发布状态</span>
            <StatPill
              active={!filters.publish_status}
              label="全部"
              count={(stats || {}).total}
              onClick={() => pickFilter('publish_status', undefined)}
            />
            {[
              ['none', '未发布'],
              ['queued', '排队中'],
              ['publishing', '发布中'],
              ['uncertain', '结果待核对'],
              ['published', '已发布'],
              ['failed', '失败'],
            ].map(([key, label]) => (
              <StatPill
                key={key}
                active={filters.publish_status === key}
                label={label}
                count={(((stats || {}).publish_status || {})[key])}
                onClick={() => pickFilter('publish_status', filters.publish_status === key ? undefined : key)}
              />
            ))}
          </div>

          <div className="stat-group">
            <span className="stat-group-label">平台审核</span>
            <StatPill
              active={!filters.review_status}
              label="全部"
              count={(stats || {}).total}
              onClick={() => pickFilter('review_status', undefined)}
            />
            {[
              ['pending', '审核中'],
              ['approved', '通过'],
              ['rejected', '不通过'],
            ].map(([key, label]) => (
              <StatPill
                key={key}
                active={filters.review_status === key}
                label={label}
                count={(((stats || {}).review_status || {})[key])}
                onClick={() => pickFilter('review_status', filters.review_status === key ? undefined : key)}
              />
            ))}
          </div>

          <div className="stat-group stat-group-platform">
            <span className="stat-group-label">平台</span>
            <StatPill
              active={!filters.platform}
              label="全部"
              count={(stats || {}).total}
              onClick={() => pickFilter('platform', undefined)}
            />
            {(((stats || {}).platforms) || []).map((p) => {
              const value = p.platform || '__none__'
              return (
                <StatPill
                  key={value}
                  active={filters.platform === value}
                  label={p.platform ? platformMeta(p.platform).label : '待定'}
                  count={p.count}
                  onClick={() => pickFilter('platform', filters.platform === value ? undefined : value)}
                />
              )
            })}
          </div>
        </div>
      </Card>

      <Card className="panel-card" bordered={false}>
        <div className="filters">
          <Input.Search
            allowClear
            placeholder="搜索标题或摘要"
            style={{ width: 240 }}
            onSearch={(v) => setQuery(v)}
          />
          <Select
            allowClear
            placeholder="文章状态"
            style={{ width: 140 }}
            value={filters.status}
            options={Object.entries(ARTICLE_STATUS).map(([value, s]) => ({ value, label: s.label }))}
            onChange={(v) => pickFilter('status', v)}
          />
          <Select
            allowClear
            placeholder="发布状态"
            style={{ width: 150 }}
            value={filters.publish_status}
            options={Object.entries(PUBLISH_STATUS).map(([value, s]) => ({ value, label: s.label }))}
            onChange={(v) => pickFilter('publish_status', v)}
          />
          <Select
            allowClear
            placeholder="平台审核"
            style={{ width: 140 }}
            value={filters.review_status}
            options={Object.entries(REVIEW_STATUS).map(([value, s]) => ({ value, label: s.label }))}
            onChange={(v) => pickFilter('review_status', v)}
          />
          <Select
            mode="tags"
            maxCount={1}
            allowClear
            showSearch
            optionFilterProp="label"
            placeholder="平台"
            style={{ width: 170 }}
            value={filters.platform ? [filters.platform] : []}
            options={Object.entries(PLATFORMS).map(([value, p]) => ({ value, label: p.label }))}
            onChange={(v) => pickFilter('platform', (v && v[0]) || undefined)}
          />
        </div>

        {activeChips.length > 0 && (
          <div className="active-filters">
            <span className="active-filters-label">当前筛选</span>
            {activeChips.map((t) => (
              <Tag key={t} className="active-filter-tag">
                {t}
              </Tag>
            ))}
            <Button type="link" size="small" onClick={clearFilters}>
              清除筛选
            </Button>
            <span className="active-filters-count">共 {total} 篇</span>
          </div>
        )}

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
            showTotal: (t) => `共 ${t} 篇`,
            onChange: (p, ps) => {
              setPageSize(ps)
              setPage(p)
            },
          }}
          onRow={(row) => ({ onClick: () => openDetail(row), style: { cursor: 'pointer' } })}
          locale={{ emptyText: <Empty description="没有符合条件的文章" /> }}
        />
      </Card>

      <Drawer
        width={720}
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        title={detail?.title || '文章详情'}
        extra={
          detail ? (
            <Space>
              <Button icon={<EditOutlined />} onClick={() => navigate(`/articles/${detail.uid}`)}>
                编辑
              </Button>
              <Button
                icon={<CopyOutlined />}
                onClick={() =>
                  api(`/articles/${detail.uid}/duplicate`, { method: 'POST' })
                    .then((d) => {
                      message.success('已复制')
                      navigate(`/articles/${d.article.uid}`)
                    })
                    .catch((e) => message.error(e.message))
                }
              >
                复制
              </Button>
            </Space>
          ) : null
        }
      >
        {detail ? (
          <div className="article-detail">
            <Descriptions column={2} size="small" bordered>
              <Descriptions.Item label="平台">
                {detail.platform ? (
                  <Tag color={platformMeta(detail.platform).color}>{platformMeta(detail.platform).label}</Tag>
                ) : (
                  <Tag>发布后确定</Tag>
                )}
              </Descriptions.Item>
              <Descriptions.Item label="账号提示">{detail.platform_account || '发布器自选'}</Descriptions.Item>
              <Descriptions.Item label="语言">{detail.language}</Descriptions.Item>
              <Descriptions.Item label="主题">{detail.topic || '—'}</Descriptions.Item>
              <Descriptions.Item label="文章状态">
                {ARTICLE_STATUS[detail.status]?.label || detail.status}
              </Descriptions.Item>
              <Descriptions.Item label="发布状态">
                <Tag color={(PUBLISH_STATUS[detail.publish_status] || {}).color}>
                  {(PUBLISH_STATUS[detail.publish_status] || {}).label || detail.publish_status}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="平台审核">
                <Tag color={(REVIEW_STATUS[detail.review_status] || REVIEW_STATUS.none).color}>
                  {(REVIEW_STATUS[detail.review_status] || REVIEW_STATUS.none).label}
                </Tag>
                {detail.review_note ? <Text type="secondary">（{detail.review_note}）</Text> : null}
              </Descriptions.Item>
              {detail.result_url ? (
                <Descriptions.Item label="发布链接" span={2}>
                  <a href={detail.result_url} target="_blank" rel="noreferrer">
                    {detail.result_url}
                  </a>
                  {detail.result_account ? <Text type="secondary">　账号：{detail.result_account}</Text> : null}
                </Descriptions.Item>
              ) : null}
              <Descriptions.Item label="字数 / 阅读">{`${detail.char_count} 字 / ${detail.reading_time} 分钟`}</Descriptions.Item>
              <Descriptions.Item label="更新时间">{fmt(detail.updated_at)}</Descriptions.Item>
              <Descriptions.Item label="标签" span={2}>
                {(detail.tags || []).length ? detail.tags.map((t) => <Tag key={t}>{t}</Tag>) : '—'}
              </Descriptions.Item>
              <Descriptions.Item label="摘要" span={2}>
                {detail.summary || '—'}
              </Descriptions.Item>
            </Descriptions>

            {detail.cover ? (
              <div className="detail-cover">
                <img src={detail.cover.url} alt="封面" />
              </div>
            ) : null}

            <div className="detail-section">
              <Text type="secondary">正文</Text>
              <div className="md-preview" dangerouslySetInnerHTML={{ __html: detail.content_html || '' }} />
            </div>
          </div>
        ) : (
          <Empty description="加载中…" />
        )}
      </Drawer>

      <Modal
        open={modalType === 'schedule'}
        title="设置排期"
        onCancel={() => setModalType(null)}
        onOk={() => form.submit()}
        destroyOnClose
      >
        <Form form={form} layout="vertical" onFinish={submitModal}>
          <Form.Item name="scheduled_at" label="发布时间" rules={[{ required: true, message: '请选择时间' }]}>
            <DatePicker showTime style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="platform_account" label="目标账号提示（可选）">
            <Input placeholder="例如：微功夫公众号" maxLength={100} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        open={modalType === 'manual'}
        title="标记为已发布"
        onCancel={() => setModalType(null)}
        onOk={() => form.submit()}
        destroyOnClose
      >
        <Form form={form} layout="vertical" onFinish={submitModal}>
          <Form.Item
            name="platform"
            label="发布平台"
            rules={[{ required: true, message: '请选择发布平台' }]}
          >
            <Select
              showSearch
              placeholder="选择平台"
              options={Object.entries(PLATFORMS).map(([value, p]) => ({ value, label: p.label }))}
            />
          </Form.Item>
          <Form.Item
            name="platform_account"
            label="实际发布账号"
            rules={[{ required: true, message: '请填写实际账号' }]}
          >
            <Input maxLength={100} placeholder="例如：微功夫公众号" />
          </Form.Item>
          <Form.Item
            name="url"
            label="发布链接"
            rules={[
              { required: true, message: '请填写链接' },
              { pattern: /^https?:\/\//, message: '必须是 http(s) 链接' },
            ]}
          >
            <Input placeholder="https://..." />
          </Form.Item>
          <Form.Item name="post_id" label="平台侧 ID（可选）">
            <Input />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
