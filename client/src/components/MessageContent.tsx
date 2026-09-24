// Renders message text safely (no HTML): ```code blocks```, `code`, **bold**,
// *italic*, links, and mentions <@member>, <@&role>, @everyone.
import type { ReactNode } from 'react'

export interface MentionNames {
  member(id: string): string | undefined
  role(id: number): { name: string; color?: string } | undefined
  me?: string
}

export function MessageContent({ text, names }: { text: string; names: MentionNames }) {
  const out: ReactNode[] = []
  const parts = text.split(/```(?:[a-z0-9]*\n)?([\s\S]*?)```/g)
  parts.forEach((part, i) => {
    if (i % 2 === 1) out.push(<pre key={i} className="md-pre"><code>{part.replace(/\n$/, '')}</code></pre>)
    else if (part) out.push(...inline(part, names, 'p' + i))
  })
  return <>{out}</>
}

const inlineRe = /(`[^`\n]+`)|(\*\*[^*\n]+\*\*)|(\*[^*\n]+\*|_[^_\n]+_)|(https?:\/\/[^\s<>"]+)|(<@&\d+>)|(<@[A-Za-z0-9]+>)|(@everyone\b)/g

function inline(text: string, names: MentionNames, key: string): ReactNode[] {
  const out: ReactNode[] = []
  let last = 0
  let n = 0
  for (const m of text.matchAll(inlineRe)) {
    if (m.index! > last) out.push(text.slice(last, m.index))
    const k = key + '-' + n++
    const [all, code, bold, italic, url, role, member, everyone] = m
    if (code) out.push(<code key={k} className="md-code">{code.slice(1, -1)}</code>)
    else if (bold) out.push(<strong key={k}>{inline(bold.slice(2, -2), names, k)}</strong>)
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
