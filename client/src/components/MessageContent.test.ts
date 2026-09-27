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

describe('MessageContent', async () => {
  const { renderToStaticMarkup } = await import('react-dom/server')
  const { createElement } = await import('react')
  const { MessageContent } = await import('./MessageContent')
  const names = { member: (id: string) => ({ m1: 'Léa' } as Record<string, string>)[id], role: () => undefined, me: 'm1' }
  const html = (text: string) => renderToStaticMarkup(createElement(MessageContent, { text, names }))

  it('keeps plain text as text', () => {
    expect(html('bonjour\nà tous')).toBe('bonjour\nà tous')
    expect(html('<b>pas de HTML</b>')).toBe('&lt;b&gt;pas de HTML&lt;/b&gt;')
  })
  it('renders inline styles', () => {
    expect(html('**gras** __souligné__ *ital* ~~barré~~ `code`')).toBe(
      '<strong>gras</strong> <u>souligné</u> <em>ital</em> <del>barré</del> <code class="md-code">code</code>')
    expect(html('nom_de_variable reste tel quel')).toBe('nom_de_variable reste tel quel')
    expect(html('fin ||secret||')).toContain('class="md-spoiler"')
  })
  it('renders blocks', () => {
    expect(html('# Titre\ntexte')).toBe('<h3 class="md-h md-h1">Titre</h3><span class="md-para">texte</span>')
    expect(html('> cité\n> deux\nsuite')).toBe('<blockquote class="md-quote">cité\ndeux</blockquote><span class="md-para">suite</span>')
    expect(html('- un\n- deux')).toBe('<ul class="md-list"><li>un</li><li>deux</li></ul>')
    expect(html('3. trois\n4. quatre')).toBe('<ol class="md-list" start="3"><li>trois</li><li>quatre</li></ol>')
    expect(html('#pas un titre')).toBe('#pas un titre')
  })
  it('renders code blocks with their language, untouched', () => {
    expect(html('avant\n```js\nconst a = **1**\n```\naprès')).toBe(
      'avant<pre class="md-pre" data-lang="js"><code>const a = **1**</code></pre>après') // the block stands on its own line
    expect(html('```\n# pas un titre\n```')).toBe('<pre class="md-pre"><code># pas un titre</code></pre>')
  })
  it('renders mentions and links', () => {
    expect(html('<@m1> https://exemple.org.')).toBe('<span class="mention me">@Léa</span> <a href="https://exemple.org" target="_blank" rel="noreferrer noopener">https://exemple.org</a>.')
  })
})
