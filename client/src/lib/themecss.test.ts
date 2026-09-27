import { describe, expect, it } from 'vitest'
import { customProps, filterCSS, parseTheme, themeCSS } from './themecss'

const S = '[data-qscope="s1"]'
const f = (css: string) => filterCSS(css, S, 's1')

describe('filterCSS', () => {
  it('keeps harmless CSS, enclosed in its area', () => {
    const r = f('.message { color: #fff; border-radius: 12px } .x:hover { background: linear-gradient(90deg, #000, #333) }')
    expect(r.dropped).toEqual([])
    expect(r.css.startsWith(`@scope (${S}) to ([data-qnotheme]) {`)).toBe(true)
    expect(r.css).toContain('.message { color: #fff; border-radius: 12px }')
    expect(r.css).toContain('linear-gradient(90deg, #000, #333)')
  })

  it('refuses anything that loads a resource', () => {
    for (const css of [
      '.a { background: url(https://evil.example/x.png) }',
      '.a { background-image: URL( "//evil.example" ) }',
      '.a { background: image-set("x.png" 1x) }',
      '.a { background: -webkit-image-set("x.png" 1x) }',
      '.a { --img: url(x) }',
      '.a { border-image: url(x) 30 }',
      '@import "x.css";',
      '@font-face { font-family: x; src: url(x.woff) }',
      '@media (min-width: 1px) { .a { background: url(x) } }',
      '@keyframes k { from { background: url(x) } }',
    ]) {
      const r = f(css)
      expect(r.css, css).not.toMatch(/url|image-set|@import|@font-face/i)
      expect(r.dropped.length, css).toBeGreaterThan(0)
    }
  })

  it('refuses escapes and markup outright', () => {
    expect(f('.a { background: u\\72l(x) }')).toEqual({ css: '', dropped: [expect.stringContaining('« \\ »')] })
    expect(f('</style><script>alert(1)</script>').css).toBe('')
    expect(f('.a { color: red } /* </style> */').css).toBe('')
  })

  it('refuses fake text and overlays', () => {
    const r = f('.a { content: "Tapez votre mot de passe"; position: fixed; z-index: 99; pointer-events: none; transform: translate(-500px); inset: 0 } .b::before { color: red }')
    expect(r.css).not.toMatch(/content|position|z-index|pointer-events|transform|inset/)
    expect(r.css).toContain('.b::before { color: red }')
    expect(r.dropped).toEqual(expect.arrayContaining(['propriété « content »', 'propriété « position »', 'propriété « z-index »']))
  })

  it('keeps out of the rest of the app', () => {
    const r = f(':root { --bg: red } html .a { color: red } body { color: red } .ok { color: blue }')
    expect(r.css).not.toMatch(/:root|html|body/)
    expect(r.css).toContain('.ok { color: blue }')
    expect(r.css).toContain('to ([data-qnotheme])')
  })

  it('removes !important, comments and unknown at-rules', () => {
    const r = f('.a { color: red !important } /* hi */ @page { margin: 0 } @layer x { .b { color: red } }')
    expect(r.css).not.toMatch(/important|hi|@page|@layer/)
    expect(r.dropped).toEqual(expect.arrayContaining(['!important (retiré)', '@page', '@layer']))
  })

  it('only allows the app’s fonts', () => {
    expect(f(".a { font-family: 'Space Grotesk', serif }").css).toContain('Space Grotesk')
    expect(f('.a { font-family: var(--font) }').css).toContain('var(--font)')
    expect(f('.a { font-family: "Comic Sans MS" }').css).not.toContain('Comic')
  })

  it('renames keyframes to this area and keeps them outside the scope', () => {
    const r = f('@keyframes glow { from { opacity: .5 } to { opacity: 1; transform: scale(2) } } .a { animation: glow 2s infinite }')
    expect(r.css).toContain('@keyframes qs1-glow')
    expect(r.css).toContain('animation: qs1-glow 2s infinite')
    expect(r.css).not.toContain('transform')
    expect(r.css.indexOf('@keyframes')).toBeGreaterThan(r.css.lastIndexOf('}\n@') - 1)
  })

  it('keeps @media and nested rules, checked the same way', () => {
    const r = f('@media (max-width: 700px) { .a { color: red; position: absolute } } .b { & .c { color: blue; content: "x" } }')
    expect(r.css).toContain('@media (max-width: 700px)')
    expect(r.css).not.toMatch(/position|content/)
    expect(r.css).toContain('color: blue')
  })

  it('limits the size and reports unreadable CSS', () => {
    expect(f('.a{color:red}'.repeat(2000)).css).toBe('')
    expect(f('.a { color: red').dropped.length + f('.a { color: red').css.length).toBeGreaterThan(0)
    expect(f('}}}').css).toBe('')
  })
})

describe('themeCSS', () => {
  it('sets the colours, the font and a tinted background', () => {
    const r = themeCSS({ colors: { accent: '#ff8800', bg: 'red' as string }, font: 'serif', gradient: { angle: 135, stops: ['#000000', '#303030'] } },
      S, 's1', { background: 'blob:app://quarel/1234-abcd', surfaces: [[':scope > .content', '--bg']] })
    expect(r.css).toContain('--accent: #ff8800;')
    expect(r.css).not.toContain('--bg: red')
    expect(r.css).toContain('--bg: rgba(17, 19, 23, 0.55)') // translucent: the picture shows through
    expect(r.css).toContain("font-family: var(--font)")
    expect(r.css).toContain(':scope > .content { background: linear-gradient(var(--bg), var(--bg)), url("blob:app://quarel/1234-abcd") center / cover no-repeat, linear-gradient(135deg, #000000, #303030); background-attachment: fixed; }')
  })

  it('only takes blob: images (never an address from the theme)', () => {
    expect(themeCSS({}, S, 's1', { background: 'https://evil.example/x.png' }).css).toBe('')
    expect(themeCSS({}, S, 's1', { background: 'blob:x") ; } .a { color: red' }).css).toBe('')
  })

  it('leaves my own colours alone when I put my CSS first', () => {
    const mine = customProps(':root { --accent: #00ff00; --bg: #000 } .x { color: red }')
    expect([...mine].sort()).toEqual(['--accent', '--bg'])
    const r = themeCSS({ colors: { accent: '#ff8800', text: '#eeeeee' } }, S, 's1', { keep: mine })
    expect(r.css).not.toContain('--accent')
    expect(r.css).toContain('--text: #eeeeee')
  })
})

describe('parseTheme', () => {
  it('keeps only what is valid', () => {
    expect(parseTheme({ colors: { accent: '#fff', nope: '#000', bg: 'red' }, font: 'comic', gradient: { angle: 3, stops: ['#000'] }, css: ' ' })).toEqual({ colors: { accent: '#fff' } })
    expect(parseTheme(null)).toBeNull()
    expect(parseTheme({ font: 'mono', css: '.a{}' })).toEqual({ font: 'mono', css: '.a{}' })
  })
})
