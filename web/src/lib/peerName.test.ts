import { describe, expect, it } from 'vitest'
import { suggestPeerName } from './peerName'

describe('suggestPeerName', () => {
  it('returns browser and device label', () => {
    const name = suggestPeerName()
    expect(name).toMatch(/ · /)
    expect(name.length).toBeGreaterThan(3)
  })
})
