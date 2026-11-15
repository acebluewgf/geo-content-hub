import React from 'react'
import ReactDOM from 'react-dom/client'
import { ConfigProvider, App as AntApp } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import dayjs from 'dayjs'
import 'dayjs/locale/zh-cn'
import App from './App'
import './styles.css'

dayjs.locale('zh-cn')

const theme = {
  token: {
    colorPrimary: '#2b5cf0',
    borderRadius: 10,
    colorBgLayout: '#f5f7fa',
    colorBorderSecondary: '#eef0f4',
    fontFamily:
      '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif',
  },
  components: {
    Layout: { siderBg: '#ffffff', headerBg: '#ffffff', bodyBg: '#f5f7fa' },
    Card: { borderRadiusLG: 10 },
    Table: { headerBg: '#fafbfc' },
    Menu: { itemBorderRadius: 8, itemHeight: 40 },
  },
}

ReactDOM.createRoot(document.getElementById('root')).render(
  <ConfigProvider locale={zhCN} theme={theme}>
    <AntApp>
      <App />
    </AntApp>
  </ConfigProvider>
)
