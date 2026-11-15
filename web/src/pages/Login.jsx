import { useState } from 'react'
import { Button, Form, Input, Alert } from 'antd'
import { LockOutlined, UserOutlined } from '@ant-design/icons'
import { api } from '../api'
import ReleaseNotes from '../components/ReleaseNotes'

export default function Login({ onLogin }) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [form] = Form.useForm()
  const remembered = localStorage.getItem('ch_username') || ''

  const submit = async (values) => {
    setLoading(true)
    setError('')
    try {
      const data = await api('/login', { method: 'POST', body: values, silent401: true })
      localStorage.setItem('ch_username', values.username)
      onLogin(data.user)
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-screen">
      <div className="login-card">
        <div className="login-logo">微</div>
        <h1>微功夫 GEO 内容中台</h1>
        <p className="login-sub">社交媒体内容仓库与发布管理</p>
        {error ? <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} /> : null}
        <Form form={form} layout="vertical" initialValues={{ username: remembered }} onFinish={submit}>
          <Form.Item name="username" label="用户名" rules={[{ required: true, message: '请输入用户名' }]}>
            <Input size="large" prefix={<UserOutlined />} placeholder="admin" autoComplete="username" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password
              size="large"
              prefix={<LockOutlined />}
              placeholder="请输入密码"
              autoComplete="current-password"
            />
          </Form.Item>
          <Form.Item style={{ marginBottom: 8 }}>
            <Button type="primary" htmlType="submit" size="large" block loading={loading}>
              登录
            </Button>
          </Form.Item>
        </Form>
        <div className="login-foot">
          <span>忘记密码请联系管理员</span>
          <ReleaseNotes compact />
        </div>
      </div>
    </div>
  )
}
