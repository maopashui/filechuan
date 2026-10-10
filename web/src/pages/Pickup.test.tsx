import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { ConfigProvider, App } from 'antd'
import { describe, expect, it, vi } from 'vitest'
import Pickup from './Pickup'
import * as api from '../api/client'

vi.mock('../api/client', async () => {
  const actual = await vi.importActual<typeof import('../api/client')>('../api/client')
  return {
    ...actual,
    verifyPickup: vi.fn(),
  }
})

describe('Pickup', () => {
  it('shows failure state on invalid code', async () => {
    vi.mocked(api.verifyPickup).mockRejectedValueOnce(new Error('fail'))
    render(
      <ConfigProvider>
        <App>
          <MemoryRouter>
            <Pickup />
          </MemoryRouter>
        </App>
      </ConfigProvider>,
    )
    await userEvent.type(screen.getByPlaceholderText(/ABCD2345/), 'BADCODE1')
    await userEvent.click(screen.getByRole('button', { name: '验证取件码' }))
    expect(await screen.findByText(/取件码无效/)).toBeInTheDocument()
  })
})
