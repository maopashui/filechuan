import { lazy, Suspense, useEffect, useState } from 'react'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { ConfigProvider, App as AntApp, theme, FloatButton } from 'antd'
import { Moon, Sun } from 'lucide-react'
import zhCN from 'antd/locale/zh_CN'
import Home from './pages/Home'
import { useAuth } from './stores/auth'
import './App.css'

/** 互联网中转相关：登录 / 管理 / 取件 / 初始化，按路由懒加载 */
const Setup = lazy(() => import('./pages/Setup'))
const Login = lazy(() => import('./pages/Login'))
const Admin = lazy(() => import('./pages/Admin'))
const Pickup = lazy(() => import('./pages/Pickup'))
/** 局域网页同样按需加载，避免拖大首页包 */
const Lan = lazy(() => import('./pages/Lan'))

function PageFallback() {
  return <div className="boot">加载中…</div>
}

export default function App() {
  const refresh = useAuth((s) => s.refresh)
  const checked = useAuth((s) => s.checked)
  const setupNeeded = useAuth((s) => s.setupNeeded)
  const [dark, setDark] = useState(() => localStorage.getItem('fc_theme') === 'dark')

  useEffect(() => {
    void refresh()
  }, [refresh])

  useEffect(() => {
    localStorage.setItem('fc_theme', dark ? 'dark' : 'light')
    document.documentElement.dataset.theme = dark ? 'dark' : 'light'
  }, [dark])

  if (!checked) {
    return <div className="boot">飞船准备起飞…</div>
  }

  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
        token: {
          colorPrimary: '#0EA5E9',
          borderRadius: 20,
          controlHeight: 40,
          fontFamily:
            '"Nunito", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif',
          fontWeightStrong: 800,
        },
        components: {
          Button: {
            borderRadius: 999,
            controlHeight: 40,
            fontWeight: 700,
          },
          Card: {
            borderRadiusLG: 22,
          },
          Input: {
            borderRadius: 14,
          },
        },
      }}
    >
      <AntApp>
        <div className={`app-shell ${dark ? 'dark' : 'light'}`}>
          <BrowserRouter>
            <Suspense fallback={<PageFallback />}>
              <Routes>
                <Route path="/" element={<Home />} />
                <Route
                  path="/setup"
                  element={setupNeeded === true ? <Setup /> : <Navigate to="/login" replace />}
                />
                <Route path="/login" element={<Login />} />
                <Route path="/admin" element={<Admin />} />
                <Route path="/pickup" element={<Pickup />} />
                <Route path="/lan" element={<Lan />} />
                <Route path="/lan/:roomCode" element={<Lan />} />
                <Route path="*" element={<Navigate to="/" replace />} />
              </Routes>
            </Suspense>
          </BrowserRouter>
          <FloatButton
            icon={dark ? <Sun size={16} /> : <Moon size={16} />}
            tooltip={dark ? '浅色' : '深色'}
            onClick={() => setDark((v) => !v)}
          />
        </div>
      </AntApp>
    </ConfigProvider>
  )
}
