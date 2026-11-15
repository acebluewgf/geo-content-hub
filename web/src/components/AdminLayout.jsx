import { useMemo } from 'react'
import { Layout, Menu, Avatar, Dropdown, Space, Typography } from 'antd'
import {
  AppstoreOutlined,
  FileTextOutlined,
  ThunderboltOutlined,
  ApiOutlined,
  SettingOutlined,
  LogoutOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'
import ReleaseNotes from './ReleaseNotes'

const { Sider, Header, Content } = Layout

const TITLES = {
  '/': '仪表盘',
  '/articles': '文章',
  '/articles/new': '新建文章',
  '/queue': '发布队列',
  '/clients': 'API 客户端',
  '/settings': '设置',
}

export default function AdminLayout({ user, onLogout }) {
  const location = useLocation()
  const navigate = useNavigate()
  const isAdmin = user?.role === 'admin'

  const items = useMemo(() => {
    const base = [
      { key: '/', icon: <AppstoreOutlined />, label: <Link to="/">仪表盘</Link> },
      { key: '/articles', icon: <FileTextOutlined />, label: <Link to="/articles">文章</Link> },
      { key: '/queue', icon: <ThunderboltOutlined />, label: <Link to="/queue">发布队列</Link> },
    ]
    if (isAdmin) {
      base.push({ key: '/clients', icon: <ApiOutlined />, label: <Link to="/clients">API 客户端</Link> })
      base.push({ key: '/settings', icon: <SettingOutlined />, label: <Link to="/settings">设置</Link> })
    }
    return base
  }, [isAdmin])

  const selected = useMemo(() => {
    const p = location.pathname
    if (p.startsWith('/articles')) return '/articles'
    if (p.startsWith('/queue')) return '/queue'
    if (p.startsWith('/clients')) return '/clients'
    if (p.startsWith('/settings')) return '/settings'
    return '/'
  }, [location.pathname])

  const title = useMemo(() => {
    const p = location.pathname
    if (p.startsWith('/articles/')) {
      return p === '/articles/new' ? '新建文章' : '编辑文章'
    }
    return TITLES[p] || TITLES[selected] || ''
  }, [location.pathname, selected])

  const menu = {
    items: [
      {
        key: 'logout',
        icon: <LogoutOutlined />,
        label: '退出登录',
        onClick: onLogout,
      },
    ],
  }

  return (
    <Layout className="app-shell">
      <Sider width={232} className="app-sider" breakpoint="lg" collapsedWidth={0}>
        <Link to="/" className="brand">
          <span className="brand-mark">微</span>
          <span className="brand-text">
            <b>微功夫</b>
            <i>GEO 内容中台</i>
          </span>
        </Link>
        <Menu mode="inline" selectedKeys={[selected]} items={items} className="app-menu" />
        <div className="sider-footer"><ReleaseNotes /></div>
      </Sider>
      <Layout>
        <Header className="app-header">
          <div className="header-left">
            <span className="crumb-muted">内容管理</span>
            <span className="crumb-sep">/</span>
            <span className="crumb-current">{title}</span>
          </div>
          <Dropdown menu={menu} trigger={['click']} placement="bottomRight">
            <Space className="header-user" size={8}>
              <Avatar size={28} icon={<UserOutlined />} style={{ background: '#e8edff', color: '#2b5cf0' }} />
              <span>{user?.display_name || user?.username}</span>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {isAdmin ? '管理员' : '编辑'}
              </Typography.Text>
            </Space>
          </Dropdown>
        </Header>
        <Content className="app-content">
          <div className="content-inner">
            <Outlet />
          </div>
        </Content>
      </Layout>
    </Layout>
  )
}
