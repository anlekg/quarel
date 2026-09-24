import { describe, expect, it } from 'vitest'
import { encodeMentions } from './MessageContent'

describe('encodeMentions', () => {
  const members = [
    { id: 'm1', display_name: 'Léa', handle: 'lea@id.example' },
    { id: 'm2', display_name: 'sam', handle: 'sam@id.example' },
  ]
  it('converts known pseudos', () => {
    expect(encodeMentions('salut @sam et @lea.', members)).toBe('salut <@m2> et <@m1>.')
  })
  it('leaves emails, unknown names and @everyone', () => {
    expect(encodeMentions('a@sam.fr @inconnu @everyone', members)).toBe('a@sam.fr @inconnu @everyone')
  })
})
