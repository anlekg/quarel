import { describe, expect, it } from 'vitest'
import { pingLevel } from './SignalBars'

describe('pingLevel', () => {
  it('maps round trips to 0-4 bars (300 ms and more: none)', () => {
    expect([undefined, 5, 59, 60, 119, 120, 199, 200, 299, 300, 900].map(pingLevel)).toEqual([-1, 4, 4, 3, 3, 2, 2, 1, 1, 0, 0])
  })
})
