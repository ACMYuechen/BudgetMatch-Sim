import { RouterProvider } from 'react-router-dom'
import { ConfigProvider, Spin } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import router from '@/router'
import '@/styles/global.css'

function App() {
  return (
    <ConfigProvider locale={zhCN} theme={{
      token: {
        colorPrimary: '#176b5b', colorInfo: '#176b5b', colorText: '#203b35',
        colorTextSecondary: '#697a74', colorBgLayout: '#f6f8f5',
        borderRadius: 10, fontFamily: 'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
      },
      components: { Button: { primaryShadow: 'none' }, Input: { activeShadow: '0 0 0 3px rgba(23,107,91,0.08)' } },
    }}>
      <RouterProvider router={router} fallbackElement={<div className="route-loading" role="status"><Spin /><span>正在打开 BudgetMatch…</span></div>} />
    </ConfigProvider>
  )
}

export default App
