import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { ConfigProvider } from 'antd'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import Home from './Home'

const authState = vi.hoisted(() => ({
  username: null as string | null,
  setupNeeded: true as boolean | null,
}))

vi.mock('../stores/auth', () => ({
  useAuth: (sel: (s: typeof authState) => unknown) => sel(authState),
}))

describe('Home', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    authState.username = null
    authState.setupNeeded = true
  })

  it('renders brand and main entries', () => {
    render(
      <ConfigProvider>
        <MemoryRouter>
          <Home />
        </MemoryRouter>
      </ConfigProvider>,
    )
    expect(screen.getByRole('heading', { name: '飞船' })).toBeInTheDocument()
    expect(screen.getByText(/让文件，轻松飞到另一台设备/)).toBeInTheDocument()
    expect(screen.getByText('局域网快传')).toBeInTheDocument()
    expect(screen.getByText('互联网中转')).toBeInTheDocument()
    expect(screen.getByText('取件')).toBeInTheDocument()
    expect(screen.getByText('首次初始化管理员')).toBeInTheDocument()
  })

  it('hides first-time setup when admin already exists', () => {
    authState.setupNeeded = false
    render(
      <ConfigProvider>
        <MemoryRouter>
          <Home />
        </MemoryRouter>
      </ConfigProvider>,
    )
    expect(screen.queryByText('首次初始化管理员')).not.toBeInTheDocument()
    expect(screen.queryByText('管理员登录')).not.toBeInTheDocument()
  })
})
