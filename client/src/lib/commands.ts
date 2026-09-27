// Slash commands typed in a channel's message box: "/name arg1 arg2…".
// Arguments fill the command's options in order; words in "double quotes"
// stay together, and a text option in last place takes the rest of the line.
// Members are written @pseudo (or <@id>), channels #name.
import type { BotCommand, CommandOption } from '../api/community'

export interface Resolver {
  member(ref: string): string | undefined // member ID
  channel(ref: string): number | undefined
}

export type Parsed = { cmd: BotCommand; options: Record<string, unknown> } | { error: string }

const NAME = /^\/([a-z0-9_-]{1,32})(?:\s|$)/

// Commands whose name starts with what follows "/" (no argument typed yet).
export function suggestCommands(text: string, commands: BotCommand[]): BotCommand[] {
  const m = /^\/([a-z0-9_-]*)$/.exec(text)
  if (!m) return []
  return commands.filter((c) => c.name.startsWith(m[1])).slice(0, 8)
}

export function usage(cmd: BotCommand): string {
  return '/' + cmd.name + cmd.options.map((o) => (o.required ? ' <' + o.name + '>' : ' [' + o.name + ']')).join('')
}

// Words with where they start in s.
function words(s: string): { text: string; at: number }[] {
  const out: { text: string; at: number }[] = []
  const re = /"([^"]*)"|(\S+)/g
  for (let m = re.exec(s); m; m = re.exec(s)) out.push({ text: m[1] ?? m[2], at: m.index })
  return out
}

function value(o: CommandOption, raw: string, r: Resolver): unknown {
  switch (o.type) {
    case 'string':
      return raw
    case 'integer':
      return /^-?\d{1,15}$/.test(raw) ? Number(raw) : undefined
    case 'boolean': {
      const v = raw.toLowerCase()
      return ['oui', 'o', 'true', 'vrai', '1'].includes(v) ? true : ['non', 'n', 'false', 'faux', '0'].includes(v) ? false : undefined
    }
    case 'member':
      return r.member(raw)
    case 'channel':
      return r.channel(raw)
  }
}

const expected: Record<CommandOption['type'], string> = {
  string: 'un texte', integer: 'un nombre entier', boolean: 'oui ou non', member: 'un membre (@pseudo)', channel: 'un salon (#nom)',
}

// null: not one of the known commands (the text is sent as a message).
export function parseCommand(text: string, commands: BotCommand[], r: Resolver): Parsed | null {
  const m = NAME.exec(text.trim())
  const cmd = m && commands.find((c) => c.name === m[1])
  if (!m || !cmd) return null
  const rest = text.trim().slice(m[0].length).trim()
  const args = words(rest)
  const options: Record<string, unknown> = {}
  for (let i = 0; i < cmd.options.length; i++) {
    const o = cmd.options[i]
    if (i >= args.length) {
      if (o.required) return { error: 'Il manque « ' + o.name + ' » : ' + usage(cmd) }
      break
    }
    let raw = args[i].text
    if (o.type === 'string' && i === cmd.options.length - 1 && args.length > i + 1) {
      raw = rest.slice(args[i].at) // the last text option takes the rest of the line as typed
      args.length = i + 1
    }
    const v = value(o, raw, r)
    if (v === undefined) return { error: '« ' + o.name + ' » attend ' + expected[o.type] + ' : ' + usage(cmd) }
    options[o.name] = v
  }
  if (args.length > cmd.options.length) return { error: 'Trop d’arguments : ' + usage(cmd) }
  return { cmd, options }
}
