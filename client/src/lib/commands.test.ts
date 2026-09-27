import { describe, expect, it } from 'vitest'
import type { BotCommand } from '../api/community'
import { parseCommand, suggestCommands, usage } from './commands'

const cmds: BotCommand[] = [
  { bot_id: 'b1', name: 'lancer', description: '', options: [
    { name: 'faces', description: '', type: 'integer', required: true },
    { name: 'pour', description: '', type: 'member', required: false },
    { name: 'secret', description: '', type: 'boolean', required: false },
  ] },
  { bot_id: 'b2', name: 'echo', description: '', options: [{ name: 'texte', description: '', type: 'string', required: true }] },
  { bot_id: 'b2', name: 'ping', description: '', options: [] },
]
const r = {
  member: (s: string) => ({ '@carol': 'm-carol', '<@m-bob>': 'm-bob' } as Record<string, string>)[s],
  channel: (s: string) => (s === '#général' ? 7 : undefined),
}

describe('slash commands', () => {
  it('suggests by prefix, only before any argument', () => {
    expect(suggestCommands('/', cmds).map((c) => c.name)).toEqual(['lancer', 'echo', 'ping'])
    expect(suggestCommands('/e', cmds).map((c) => c.name)).toEqual(['echo'])
    expect(suggestCommands('/echo x', cmds)).toEqual([])
    expect(suggestCommands('bonjour', cmds)).toEqual([])
  })
  it('parses options in order, typed', () => {
    expect(parseCommand('/lancer 20 @carol oui', cmds, r)).toEqual({ cmd: cmds[0], options: { faces: 20, pour: 'm-carol', secret: true } })
    expect(parseCommand('/lancer 6', cmds, r)).toEqual({ cmd: cmds[0], options: { faces: 6 } })
    expect(parseCommand('/ping', cmds, r)).toEqual({ cmd: cmds[2], options: {} })
  })
  it('gives the rest of the line to a last text option', () => {
    expect(parseCommand('/echo  bonjour   à "tous" ', cmds, r)).toEqual({ cmd: cmds[1], options: { texte: 'bonjour   à "tous"' } })
    expect(parseCommand('/echo "entre guillemets"', cmds, r)).toEqual({ cmd: cmds[1], options: { texte: 'entre guillemets' } })
  })
  it('explains mistakes', () => {
    expect(parseCommand('/lancer', cmds, r)).toEqual({ error: 'Il manque « faces » : /lancer <faces> [pour] [secret]' })
    expect(parseCommand('/lancer six', cmds, r)).toMatchObject({ error: expect.stringContaining('nombre entier') })
    expect(parseCommand('/lancer 6 @inconnu', cmds, r)).toMatchObject({ error: expect.stringContaining('membre') })
    expect(parseCommand('/ping encore', cmds, r)).toMatchObject({ error: expect.stringContaining('Trop') })
    expect(usage(cmds[1])).toBe('/echo <texte>')
  })
  it('leaves other text alone', () => {
    expect(parseCommand('/inconnu 3', cmds, r)).toBeNull()
    expect(parseCommand('bonjour /ping', cmds, r)).toBeNull()
  })
})
