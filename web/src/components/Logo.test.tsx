import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Logo } from './Logo'

describe('Logo', () => {
  it('exposes accessible brand label', () => {
    render(<Logo />)
    expect(screen.getByRole('img', { name: '飞船' })).toBeInTheDocument()
  })
})
