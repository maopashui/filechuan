import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { ConfigProvider, App } from 'antd'
import { describe, expect, it, vi } from 'vitest'
import Login from './Login'

vi.mock('../stores/auth', () => ({
  useAuth: (sel: (s: { login: () => Promise<void> }) => unknown) =>
    sel({ login: vi.fn(async () => {}) }),
}))

describe('Login', () => {
  it('renders login form', () => {
    render(
      <ConfigProvider>
        <App>
          <MemoryRouter>
            <Login />
          </MemoryRouter>
        </App>
      </ConfigProvider>,
    )
    expect(screen.getByRole('heading', { name: '管理员登录' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /登\s*录/ })).toBeInTheDocument()
  })
})
