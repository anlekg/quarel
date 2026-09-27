// Renders message text safely (no HTML), in the same Markdown flavour on
// community servers and in private messages:
//   blocks: ```code blocks``` (optional language), # / ## / ### titles,
//           > quotes, - or * lists, 1. numbered lists;
//   inline: `code`, **bold**, __underline__, *italic* or _italic_,
//           ~~strikethrough~~, ||spoiler|| (hidden until clicked), links,
//           and mentions <@member>, <@&role>, @everyone.
// No masked links ([text](address)): the address shown is the one opened.
import { useState, type ReactNode } from 'react'

export interface MentionNames {
  member(id: string): string | undefined
  role(id: number): { name: string; color?: string } | undefined
  me?: string
}

export function MessageContent({ text, names }: { text: string; names: MentionNames }) {
  const out: ReactNode[] = []
  const parts = text.split(/```([a-z0-9+#-]*\n)?([\s\S]*?)```/gi)
  // split with two groups: text, language, code, text, language, code…
  for (let i = 0; i < parts.length; i += 3) {
    if (parts[i]) out.push(...blocks(parts[i], names, 'b' + i))
    if (i + 2 < parts.length) {
      const lang = (parts[i + 1] ?? '').trim()
      out.push(
        <pre key={'c' + i} className="md-pre" data-lang={lang || undefined}>
          <code>{(parts[i + 2] ?? '').replace(/^\n/, '').replace(/\n$/, '')}</code>
        </pre>,
      )
    }
  }
  return <>{out}</>
}

type Block =
  | { kind: 'text'; lines: string[] }
  | { kind: 'quote'; lines: string[] }
  | { kind: 'ul'; items: string[] }
  | { kind: 'ol'; start: number; items: string[] }
  | { kind: 'h'; level: 1 | 2 | 3; text: string }

// Groups lines into blocks; ordinary lines keep their line breaks.
function parse(text: string): Block[] {
  const out: Block[] = []
  const last = () => out[out.length - 1]
  for (const line of text.split('\n')) {
    let m: RegExpExecArray | null
    if ((m = /^(#{1,3}) +(\S.*)$/.exec(line))) out.push({ kind: 'h', level: m[1].length as 1 | 2 | 3, text: m[2] })
    else if ((m = /^> ?(.*)$/.exec(line))) {
      const b = last()
      if (b?.kind === 'quote') b.lines.push(m[1])
      else out.push({ kind: 'quote', lines: [m[1]] })
    } else if ((m = /^ {0,3}[-*•] +(\S.*)$/.exec(line))) {
      const b = last()
      if (b?.kind === 'ul') b.items.push(m[1])
      else out.push({ kind: 'ul', items: [m[1]] })
    } else if ((m = /^ {0,3}(\d{1,9})[.)] +(\S.*)$/.exec(line))) {
      const b = last()
      if (b?.kind === 'ol') b.items.push(m[2])
      else out.push({ kind: 'ol', start: Number(m[1]), items: [m[2]] })
    } else {
      const b = last()
      if (b?.kind === 'text') b.lines.push(line)
      else out.push({ kind: 'text', lines: [line] })
    }
  }
  // Empty lines around blocks come from the separating line breaks.
  return out.filter((b) => b.kind !== 'text' || b.lines.some((l) => l !== '')).map((b) => {
    if (b.kind !== 'text') return b
    const lines = [...b.lines]
    while (lines[0] === '') lines.shift()
    while (lines[lines.length - 1] === '') lines.pop()
    return { kind: 'text', lines }
  })
}

function blocks(text: string, names: MentionNames, key: string): ReactNode[] {
  const list = parse(text)
  if (list.length === 1 && list[0].kind === 'text') return inline(list[0].lines.join('\n'), names, key)
  return list.map((b, i) => {
    const k = key + '-' + i
    switch (b.kind) {
      case 'text':
        return <span key={k} className="md-para">{inline(b.lines.join('\n'), names, k)}</span>
      case 'quote':
        return <blockquote key={k} className="md-quote">{inline(b.lines.join('\n'), names, k)}</blockquote>
      case 'ul':
        return <ul key={k} className="md-list">{b.items.map((it, j) => <li key={j}>{inline(it, names, k + j)}</li>)}</ul>
      case 'ol':
        return <ol key={k} className="md-list" start={b.start}>{b.items.map((it, j) => <li key={j}>{inline(it, names, k + j)}</li>)}</ol>
      case 'h': {
        const H = (['h3', 'h4', 'h5'] as const)[b.level - 1]
        return <H key={k} className={'md-h md-h' + b.level}>{inline(b.text, names, k)}</H>
      }
    }
  })
}

const inlineRe = new RegExp([
  /(`[^`\n]+`)/, // 1 code
  /(\|\|[^|\n]+\|\|)/, // 2 spoiler
  /(\*\*[^*\n]+\*\*)/, // 3 bold
  /(__[^_\n]+__)/, // 4 underline
  /(~~[^~\n]+~~)/, // 5 strikethrough
  /(\*[^*\n]+\*|(?<![A-Za-z0-9])_[^_\n]+_(?![A-Za-z0-9]))/, // 6 italic (not inside snake_case words)
  /(https?:\/\/[^\s<>"]+)/, // 7 link
  /(<@&\d+>)/, // 8 role
  /(<@[A-Za-z0-9]+>)/, // 9 member
  /(@everyone\b)/, // 10
].map((r) => r.source).join('|'), 'g')

function inline(text: string, names: MentionNames, key: string): ReactNode[] {
  const out: ReactNode[] = []
  let last = 0
  let n = 0
  for (const m of text.matchAll(inlineRe)) {
    if (m.index! > last) out.push(text.slice(last, m.index))
    const k = key + '-' + n++
    const [all, code, spoiler, bold, underline, strike, italic, url, role, member, everyone] = m
    if (code) out.push(<code key={k} className="md-code">{code.slice(1, -1)}</code>)
    else if (spoiler) out.push(<Spoiler key={k}>{inline(spoiler.slice(2, -2), names, k)}</Spoiler>)
    else if (bold) out.push(<strong key={k}>{inline(bold.slice(2, -2), names, k)}</strong>)
    else if (underline) out.push(<u key={k}>{inline(underline.slice(2, -2), names, k)}</u>)
    else if (strike) out.push(<del key={k}>{inline(strike.slice(2, -2), names, k)}</del>)
    else if (italic) out.push(<em key={k}>{inline(italic.slice(1, -1), names, k)}</em>)
    else if (url) {
      const clean = url.replace(/[.,;:!?)]+$/, '')
      out.push(<a key={k} href={clean} target="_blank" rel="noreferrer noopener">{clean}</a>)
      if (clean.length < url.length) out.push(url.slice(clean.length))
    } else if (role) {
      const r = names.role(Number(role.slice(3, -1)))
      out.push(<span key={k} className="mention" style={r?.color ? { color: r.color } : undefined}>@{r?.name ?? 'rôle supprimé'}</span>)
    } else if (member) {
      const id = member.slice(2, -1)
      out.push(<span key={k} className={'mention' + (id === names.me ? ' me' : '')}>@{names.member(id) ?? 'ancien membre'}</span>)
    } else if (everyone) out.push(<span key={k} className="mention">@everyone</span>)
    else out.push(all)
    last = m.index! + all.length
  }
  if (last < text.length) out.push(text.slice(last))
  return out
}

function Spoiler({ children }: { children: ReactNode }) {
  const [shown, setShown] = useState(false)
  return (
    <span className={'md-spoiler' + (shown ? ' shown' : '')} role={shown ? undefined : 'button'} tabIndex={shown ? undefined : 0}
      aria-label={shown ? undefined : 'Divulgâcheur : afficher'} onClick={() => setShown(true)}
      onKeyDown={(e) => (e.key === 'Enter' || e.key === ' ') && setShown(true)}>
      <span aria-hidden={!shown}>{children}</span>
    </span>
  )
}

// Turns "@pseudo" typed in the composer into mentions the server understands.
export function encodeMentions(text: string, members: { id: string; display_name: string; handle: string }[]) {
  const byName = new Map<string, string>()
  for (const m of members) {
    byName.set(m.display_name.toLowerCase(), m.id)
    byName.set(m.handle.split('@')[0].toLowerCase(), m.id)
  }
  return text.replace(/(^|[\s(])@([A-Za-z0-9_.-]{2,32})/g, (all, pre, name) => {
    if (name.toLowerCase() === 'everyone') return all
    const id = byName.get(name.toLowerCase()) ?? byName.get(name.replace(/[.-]+$/, '').toLowerCase())
    return id ? pre + '<@' + id + '>' + name.slice(name.replace(/[.-]+$/, '').length) : all
  })
}
