import { useEffect, useState } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { App as AntApp, Spin } from 'antd'
import { api } from './api'
import AdminLayout from './components/AdminLayout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Articles from './pages/Articles'
import Editor from './pages/Editor'
import Queue from './pages/Queue'
import Clients from './pages/Clients'
import Settings from './pages/Settings'

export default function App() {
  const [user, setUser] = useState(undefined)
  const { message } = AntApp.useApp()

  useEffect(() => {
    api('/me', { silent401: true })
      .then((d) => setUser(d.user))
      .catch(() => setUser(null))
    const onExpired = () => setUser(null)
    window.addEventListener('auth:expired', onExpired)
    return () => window.removeEventListener('auth:expired', onExpired)
  }, [])

  const logout = async () => {
    try {
      await api('/logout', { method: 'POST' })
      setUser(null)
    } catch (e) {
      message.error('退出未完成：' + e.message)
    }
  }

  if (user === undefined) {
    return (
      <div className="boot-screen">
        <Spin size="large" />
      </div>
    )
  }

  return (
    <BrowserRouter basename="/admin/">
      <Routes>
        <Route path="/login" element={user ? <Navigate to="/" replace /> : <Login onLogin={setUser} />} />
        <Route
          element={user ? <AdminLayout user={user} onLogout={logout} /> : <Navigate to="/login" replace />}
        >
          <Route index element={<Dashboard />} />
          <Route path="articles" element={<Articles />} />
          <Route path="articles/new" element={<Editor />} />
          <Route path="articles/:uid" element={<Editor />} />
          <Route path="queue" element={<Queue />} />
          <Route path="clients" element={<Clients user={user} />} />
          <Route path="settings" element={<Settings user={user} />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
