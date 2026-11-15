import { useEffect, useRef, useState } from 'react'
import dayjs from 'dayjs'
import {
  App as AntApp,
  Button,
  Card,
  Col,
  DatePicker,
  Form,
  Input,
  Row,
  Segmented,
  Select,
  Space,
  Spin,
  Tag,
  Typography,
  Upload,
} from 'antd'
import { ArrowLeftOutlined, PictureOutlined, SaveOutlined, SendOutlined } from '@ant-design/icons'
import { useNavigate, useParams } from 'react-router-dom'
import { api, uploadImage } from '../api'
import { LANGUAGES } from '../utils'

const { TextArea } = Input
const { Text } = Typography

export default function Editor() {
  const { uid } = useParams()
  const isNew = !uid || uid === 'new'
  const navigate = useNavigate()
  const { message } = AntApp.useApp()
  const [form] = Form.useForm()
  const [loading, setLoading] = useState(!isNew)
  const [saving, setSaving] = useState(false)
  const [html, setHtml] = useState('')
  const [meta, setMeta] = useState({ chars: 0, minutes: 0 })
  const [cover, setCover] = useState(null)
  const [uploading, setUploading] = useState(false)
  const [revision, setRevision] = useState(null)
  const request = useRef(null)
  const savingRef = useRef(false)
  const timer = useRef(null)
  const bodyMD = Form.useWatch('body_md', form) || ''

  useEffect(() => {
    const controller = new AbortController()
    setLoading(!isNew)
    setRevision(null)
    setCover(null)
    form.resetFields()
    if (!isNew) {
      api('/articles/' + uid, { signal: controller.signal })
        .then((d) => {
          const a = d.article
          form.setFieldsValue({
            platform_account: a.platform_account,
            language: a.language,
            topic: a.topic,
            tags: a.tags || [],
            title: a.title,
            summary: a.summary,
            body_md: a.content_md,
            publish_mode: a.publish_mode,
            status: a.status,
            scheduled_at: a.scheduled_at ? dayjs(a.scheduled_at) : null,
          })
          setRevision(a.revision)
          if (a.cover) setCover(a.cover)
          setHtml(a.content_html || '')
        })
        .catch((e) => { if (!controller.signal.aborted) message.error(e.message) })
        .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    }
    return () => controller.abort()
  }, [isNew, uid])

  useEffect(() => {
    const controller = new AbortController()
    if (!bodyMD) {
      setHtml('')
      setMeta({ chars: 0, minutes: 0 })
      return
    }
    clearTimeout(timer.current)
    timer.current = setTimeout(() => {
      api('/preview', { method: 'POST', body: { body_md: bodyMD }, signal: controller.signal })
        .then((d) => {
          setHtml(d.html)
          setMeta({ chars: d.chars, minutes: d.minutes })
        })
        .catch(() => {})
    }, 450)
    return () => { clearTimeout(timer.current); controller.abort() }
  }, [bodyMD])

  const beforeUpload = async (file) => {
    setUploading(true)
    try {
      const d = await uploadImage(file)
      setCover(d.media)
      message.success('封面已上传')
    } catch (e) {
      message.error(e.message)
    } finally {
      setUploading(false)
    }
    return false
  }

  const insertInlineImage = async (file) => {
    try {
      const d = await uploadImage(file)
      const md = form.getFieldValue('body_md') || ''
      form.setFieldsValue({ body_md: `${md}\n\n![图片](media:${d.media.uid})\n` })
      message.success('图片已插入正文')
    } catch (e) {
      message.error(e.message)
    }
    return false
  }

  const save = async (publishReady) => {
    if (savingRef.current || loading || uploading || (!isNew && !revision)) return
    let values
    try {
      values = await form.validateFields()
    } catch (e) {
      return
    }
    setSaving(true)
    savingRef.current = true
    try {
      const payload = {
        ...values,
        status: publishReady ? 'ready' : 'draft',
        cover: cover ? { uid: cover.uid } : null,
        scheduled_at: values.scheduled_at ? values.scheduled_at.toISOString() : null,
        ...(!isNew ? { revision, clear_schedule: !values.scheduled_at } : {}),
      }
      const fingerprint = JSON.stringify(payload)
      if (request.current?.fingerprint !== fingerprint) {
        request.current = { fingerprint, key: `editor-${Date.now()}-${Math.random().toString(36).slice(2)}` }
      }
      let article
      if (isNew) {
        article = (await api('/articles', { method: 'POST', body: payload, idempotencyKey: request.current.key })).article
      } else {
        article = (await api(`/articles/${uid}`, { method: 'PATCH', body: payload, idempotencyKey: request.current.key })).article
      }
      setRevision(article.revision)
      message.success(publishReady ? '已保存并进入待发布' : '已保存')
      navigate('/articles')
    } catch (e) {
      message.error(e.message)
    } finally {
      setSaving(false)
      savingRef.current = false
    }
  }

  return (
    <div className="page">
      <div className="page-head">
        <Space>
          <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/articles')} />
          <div>
            <Typography.Title level={4} style={{ margin: 0 }}>
              {isNew ? '新建文章' : '编辑文章'}
            </Typography.Title>
            <Text type="secondary">Markdown 正文，右侧为服务端渲染预览；平台由发布器回执确定</Text>
          </div>
        </Space>
        <Space>
          <Button icon={<SaveOutlined />} loading={saving} disabled={loading || uploading || (!isNew && !revision)} onClick={() => save(false)}>
            保存草稿
          </Button>
          <Button type="primary" icon={<SendOutlined />} loading={saving} disabled={loading || uploading || (!isNew && !revision)} onClick={() => save(true)}>
            保存并待发布
          </Button>
        </Space>
      </div>

      <Spin spinning={loading}>
        <Form form={form} layout="vertical" initialValues={{ language: 'zh-CN', publish_mode: 'agent', status: 'draft' }}>
          <Row gutter={16}>
            <Col xs={24} xl={14}>
              <Card className="panel-card" bordered={false} title="基础信息">
                <Row gutter={12}>
                  <Col span={12}>
                    <Form.Item name="platform_account" label="目标账号提示（可选）">
                      <Input placeholder="发布器自选，可留空" maxLength={100} />
                    </Form.Item>
                  </Col>
                  <Col span={6}>
                    <Form.Item name="language" label="语言">
                      <Select options={LANGUAGES} />
                    </Form.Item>
                  </Col>
                  <Col span={6}>
                    <Form.Item name="publish_mode" label="发布方式">
                      <Select
                        options={[
                          { value: 'agent', label: '智能体发布' },
                          { value: 'manual', label: '手动发布' },
                        ]}
                      />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item name="topic" label="主题">
                      <Input placeholder="例如：外贸增长" maxLength={100} />
                    </Form.Item>
                  </Col>
                  <Col span={24}>
                    <Form.Item name="title" label="标题" rules={[{ required: true, message: '请填写标题' }]}>
                      <Input size="large" maxLength={200} showCount />
                    </Form.Item>
                  </Col>
                  <Col span={24}>
                    <Form.Item name="summary" label="摘要">
                      <TextArea rows={2} maxLength={500} showCount />
                    </Form.Item>
                  </Col>
                  <Col span={24}>
                    <Form.Item name="tags" label="标签">
                      <Select mode="tags" tokenSeparators={[',', '，']} placeholder="输入后回车添加" />
                    </Form.Item>
                  </Col>
                  <Col span={24}>
                    <Form.Item label="封面">
                      <Space align="start">
                        <Upload showUploadList={false} accept="image/*" beforeUpload={beforeUpload}>
                          <Button icon={<PictureOutlined />} loading={uploading}>
                            {cover ? '更换封面' : '上传封面'}
                          </Button>
                        </Upload>
                        {cover ? (
                          <div className="cover-thumb">
                            <img src={cover.url} alt="封面" />
                            <a onClick={() => setCover(null)}>移除</a>
                          </div>
                        ) : (
                          <Text type="secondary">建议比例 16:9，自动压缩为 JPEG/PNG</Text>
                        )}
                      </Space>
                    </Form.Item>
                  </Col>
                </Row>
              </Card>

              <Card
                className="panel-card"
                bordered={false}
                title="正文（Markdown）"
                style={{ marginTop: 16 }}
                extra={
                  <Space>
                    <Upload showUploadList={false} accept="image/*" beforeUpload={insertInlineImage}>
                      <Button size="small" icon={<PictureOutlined />}>
                        插入图片
                      </Button>
                    </Upload>
                  </Space>
                }
              >
                <Form.Item name="body_md" noStyle>
                  <TextArea className="md-input" rows={22} placeholder={'# 标题\n\n正文…'} />
                </Form.Item>
                <Form.Item name="scheduled_at" label="排期（可选）" style={{ marginTop: 16, marginBottom: 0 }}>
                  <DatePicker showTime style={{ width: 260 }} />
                </Form.Item>
              </Card>
            </Col>

            <Col xs={24} xl={10}>
              <div className="preview-sticky">
                <Card
                  className="panel-card"
                  bordered={false}
                  title="预览"
                  extra={
                    <Text type="secondary">
                      {meta.chars} 字 · {meta.minutes} 分钟
                    </Text>
                  }
                >
                  <div className="preview-title">{form.getFieldValue('title') || '未命名文章'}</div>
                  {cover ? <img className="preview-cover" src={cover.url} alt="封面" /> : null}
                  <div
                    className="md-preview"
                    dangerouslySetInnerHTML={{ __html: html || '<p style="color:#9aa3b2">开始输入正文…</p>' }}
                  />
                </Card>
              </div>
            </Col>
          </Row>
        </Form>
      </Spin>
    </div>
  )
}
