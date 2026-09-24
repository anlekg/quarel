import { describe, expect, it } from 'vitest'
import type { Channel } from '../api/community'
import { channelTree, firstTextChannel } from './community'

const ch = (id: number, type: Channel['type'], parent: number | null, position = 0): Channel =>
  ({ id, type, name: 'c' + id, topic: '', parent_id: parent, position })

describe('channelTree', () => {
  const channels = [ch(1, 'category', null, 0), ch(2, 'text', 1, 0), ch(3, 'category', null, 1), ch(4, 'voice', 3), ch(5, 'thread', 2), ch(6, 'text', null, 5)]
  it('groups channels under categories, threads under channels', () => {
    const t = channelTree(channels)
    expect(t.map((g) => g.category?.id ?? null)).toEqual([null, 1, 3])
    expect(t[0].items.map((n) => n.channel.id)).toEqual([6])
    expect(t[1].items[0].threads.map((c) => c.id)).toEqual([5])
  })
  it('finds the first text channel in display order', () => {
    expect(firstTextChannel(channels)?.id).toBe(6)
  })
})
