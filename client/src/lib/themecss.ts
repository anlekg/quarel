// Themes (cosmetics block): colours, a gradient, a font of the app and CSS,
// for a server's area or a profile card. The CSS comes from other people (a
// server's managers, a member): it is parsed and only what is harmless is
// kept, enclosed in its area with @scope. What is refused, and why:
// - loading anything (url(), image-set(), @import, @font-face…): it would give
//   the viewer's IP address, and with attribute selectors what is on screen;
// - escapes (\) and "<": they would hide the above from these checks;
// - content, position, z-index, pointer-events, transform…: fake text and
//   buttons over the app (phishing);
// - !important: the viewer's own choices keep the last word.
// The servers check the same basics (internal/theme); this filter is the one
// that counts, since a server is not trusted. My own CSS is not filtered.
import postcss, { type AtRule, type ChildNode, type Declaration, type Root, type Rule } from 'postcss'

export interface Theme {
  colors?: Partial<Record<ThemeColor, string>>
  gradient?: { angle: number; stops: string[] }
  font?: ThemeFont
  css?: string
}

export const themeColors = ['bg_deep', 'bg', 'bg_1', 'bg_2', 'bg_3', 'line', 'text', 'text_2', 'text_3', 'accent', 'accent_ink', 'accent_text'] as const
export type ThemeColor = (typeof themeColors)[number]
export const themeFonts = { manrope: "'Manrope', system-ui, sans-serif", 'space-grotesk': "'Space Grotesk', 'Manrope', sans-serif", system: 'system-ui, sans-serif', serif: "Georgia, 'Times New Roman', serif", mono: 'ui-monospace, Consolas, monospace' } as const
export type ThemeFont = keyof typeof themeFonts

// The app's variable of each colour (styles/app.css).
export const colorVar = (c: ThemeColor) => '--' + c.replace('_', '-')

const hexColor = /^#([0-9a-f]{3}|[0-9a-f]{6}|[0-9a-f]{8})$/i
export const isColor = (v: unknown): v is string => typeof v === 'string' && hexColor.test(v)

// Properties another person's CSS may set. Custom properties (--x) too: that
// is how a theme changes the app's colours.
const allowed = new Set([
  'color', 'background', 'background-color', 'background-image', 'background-position', 'background-size', 'background-repeat',
  'background-attachment', 'background-blend-mode', 'background-clip', '-webkit-background-clip', 'background-origin',
  '-webkit-text-fill-color', '-webkit-text-stroke', 'caret-color', 'accent-color', 'fill', 'stroke',
  'border', 'border-top', 'border-right', 'border-bottom', 'border-left', 'border-color', 'border-style', 'border-width',
  'border-top-color', 'border-right-color', 'border-bottom-color', 'border-left-color',
  'border-top-style', 'border-right-style', 'border-bottom-style', 'border-left-style',
  'border-top-width', 'border-right-width', 'border-bottom-width', 'border-left-width',
  'border-radius', 'border-top-left-radius', 'border-top-right-radius', 'border-bottom-left-radius', 'border-bottom-right-radius',
  'outline', 'outline-color', 'outline-style', 'outline-width', 'outline-offset',
  'box-shadow', 'text-shadow', 'opacity', 'filter', 'backdrop-filter', 'mix-blend-mode',
  'font-family', 'font-size', 'font-weight', 'font-style', 'font-variant', 'font-feature-settings', 'letter-spacing', 'word-spacing',
  'line-height', 'text-transform', 'text-decoration', 'text-decoration-line', 'text-decoration-style', 'text-decoration-color',
  'text-decoration-thickness', 'text-underline-offset', 'text-align', 'white-space',
  'padding', 'padding-top', 'padding-right', 'padding-bottom', 'padding-left', 'padding-inline', 'padding-block',
  'margin', 'margin-top', 'margin-right', 'margin-bottom', 'margin-left', 'margin-inline', 'margin-block',
  'gap', 'row-gap', 'column-gap', 'width', 'height', 'min-width', 'min-height', 'max-width', 'max-height',
  'display', 'visibility', 'flex-direction', 'flex-wrap', 'justify-content', 'align-items', 'align-self',
  'transition', 'transition-property', 'transition-duration', 'transition-timing-function', 'transition-delay',
  'animation', 'animation-name', 'animation-duration', 'animation-timing-function', 'animation-delay', 'animation-iteration-count',
  'animation-direction', 'animation-fill-mode', 'animation-play-state',
  'cursor', 'list-style-type', 'scrollbar-color', 'scrollbar-width',
])
// Keyframes may only animate what cannot move things around.
const allowedInKeyframes = new Set(['color', 'background', 'background-color', 'background-image', 'background-position', 'background-size',
  'border-color', 'box-shadow', 'text-shadow', 'opacity', 'filter', 'backdrop-filter', 'outline-color'])

const fonts = new Set(['manrope', 'space grotesk', 'system-ui', 'sans-serif', 'serif', 'monospace', 'ui-monospace', 'georgia',
  'times new roman', 'consolas', 'cursive', 'inherit', 'initial'])
const loads = /url\s*\(|image-set|image\s*\(|element\s*\(|src\s*\(|attr\s*\(|expression|javascript:|@import/i
const maxSize = 16 * 1024

export interface Filtered {
  css: string // ready to insert: enclosed in the scope
  dropped: string[] // what was refused (in French, for the editor)
}

function fontOK(value: string) {
  if (/^var\(--[\w-]+\)$/.test(value.trim())) return true
  return value.split(',').every((f) => fonts.has(f.trim().replace(/^['"]|['"]$/g, '').toLowerCase()))
}

// Filters someone else's CSS for the area selected by scope (a selector), up
// to the elements marked data-qnotheme (the app's own, inside the area).
export function filterCSS(input: string, scope: string, id: string): Filtered {
  const dropped: string[] = []
  const drop = (what: string) => { if (!dropped.includes(what) && dropped.length < 50) dropped.push(what) }
  if (!input.trim()) return { css: '', dropped }
  if (input.length > maxSize) return { css: '', dropped: ['tout : plus de 16 Ko'] }
  if (/[\\<]/.test(input)) return { css: '', dropped: ['tout : les caractères « \\ » et « < » sont refusés'] }
  let root: Root
  try {
    root = postcss.parse(input)
  } catch (e) {
    return { css: '', dropped: ['tout : CSS illisible (' + ((e as { reason?: string }).reason ?? 'erreur') + ')'] }
  }
  const keyframes: string[] = []
  const renamed = new Map<string, string>()
  const prefix = 'q' + id.replace(/[^a-z0-9]/gi, '') + '-'
  root.walkAtRules('keyframes', (a) => {
    if (/^[a-z][\w-]*$/i.test(a.params)) renamed.set(a.params, prefix + a.params)
  })

  const cleanDecl = (d: Declaration, inKeyframes: boolean) => {
    const prop = d.prop.toLowerCase()
    const custom = prop.startsWith('--')
    if (!custom && !(inKeyframes ? allowedInKeyframes : allowed).has(prop)) return drop('propriété « ' + prop + ' »'), d.remove()
    if (loads.test(d.value)) return drop('chargement externe (url(), image-set()…)'), d.remove()
    if (prop === 'font-family' && !fontOK(d.value)) return drop('police « ' + d.value + ' » (seulement celles de l’application)'), d.remove()
    if (prop === 'display' && !/^(none|block|inline|inline-block|flex|inline-flex|grid|contents)$/i.test(d.value.trim())) return drop('display « ' + d.value + ' »'), d.remove()
    if (d.important) drop('!important (retiré)')
    d.important = false
    if (prop === 'animation' || prop === 'animation-name') {
      d.value = d.value.replace(/[a-z][\w-]*/gi, (w) => renamed.get(w) ?? w)
    }
  }
  const walk = (nodes: ChildNode[], inKeyframes: boolean) => {
    for (const n of [...nodes]) {
      if (n.type === 'comment') n.remove()
      else if (n.type === 'decl') cleanDecl(n, inKeyframes)
      else if (n.type === 'rule') {
        const sel = (n as Rule).selector
        if (/:root|:host|::?backdrop|\bhtml\b|\bbody\b/i.test(sel)) { drop('sélecteur « ' + sel + ' »'); n.remove(); continue }
        walk((n as Rule).nodes, inKeyframes)
      } else if (n.type === 'atrule') {
        const a = n as AtRule
        const name = a.name.toLowerCase()
        if (name === 'keyframes' && !inKeyframes && renamed.has(a.params)) {
          walk(a.nodes ?? [], true)
          a.params = renamed.get(a.params)!
          keyframes.push(a.toString())
          a.remove()
        } else if ((name === 'media' || name === 'supports') && !inKeyframes && a.nodes) {
          if (loads.test(a.params)) { drop('@' + name + ' « ' + a.params + ' »'); a.remove(); continue }
          walk(a.nodes, false)
        } else {
          drop('@' + name)
          a.remove()
        }
      }
    }
  }
  walk(root.nodes, false)
  const body = root.toString().trim()
  // Belt and braces: whatever went through the checks still cannot load anything.
  if (loads.test(body) || loads.test(keyframes.join(''))) return { css: '', dropped: ['tout : chargement externe'] }
  const css = (body ? `@scope (${scope}) to ([data-qnotheme]) {\n${body}\n}\n` : '') + keyframes.join('\n')
  return { css, dropped }
}

// Declarations of the custom properties my own CSS sets (they win over a
// server's colours when I put my CSS first).
export function customProps(css: string): Set<string> {
  const out = new Set<string>()
  try {
    postcss.parse(css).walkDecls((d) => { if (d.prop.startsWith('--')) out.add(d.prop) })
  } catch { /* my CSS does not parse: nothing to protect */ }
  return out
}

export interface ThemeOptions {
  background?: string // blob: URL of the server's background image
  keep?: Set<string> // custom properties not to override (my own CSS first)
  // Elements that show the gradient or image (selectors inside the scope),
  // each under a tint of one of the app's colours: text stays readable.
  surfaces?: [selector: string, tint: string][]
}

// The CSS of a theme: its colours and font on the area, its gradient or image
// behind it, then its filtered CSS.
export function themeCSS(theme: Theme | null | undefined, scope: string, id: string, opts: ThemeOptions = {}): Filtered {
  const vars: string[] = []
  const set = (name: string, value: string) => { if (!opts.keep?.has(name)) vars.push(`${name}: ${value};`) }
  for (const c of themeColors) {
    const v = theme?.colors?.[c]
    if (isColor(v)) set(colorVar(c), v)
  }
  const layers: string[] = []
  if (opts.background && /^blob:[\w:/.-]+$/.test(opts.background)) layers.push(`url("${opts.background}") center / cover no-repeat`)
  const g = theme?.gradient
  if (g && Number.isFinite(g.angle) && g.stops?.length >= 2 && g.stops.every(isColor)) layers.push(`linear-gradient(${Math.round(g.angle) % 361}deg, ${g.stops.join(', ')})`)
  if (layers.length) {
    // The panels let the background through, unless the theme set their colour.
    if (!isColor(theme?.colors?.bg)) set('--bg', 'rgba(17, 19, 23, 0.55)')
    if (!isColor(theme?.colors?.bg_1)) set('--bg-1', 'rgba(22, 25, 31, 0.62)')
  }
  const font = theme?.font && themeFonts[theme.font]
  if (font) set('--font', font), set('--font-display', font)
  let out = ''
  if (vars.length) out += `@scope (${scope}) to ([data-qnotheme]) {\n:scope { ${vars.join(' ')}${font ? ' font-family: var(--font);' : ''} }\n}\n`
  if (layers.length) {
    // Fixed to the window: the panels of an area show one continuous picture.
    const rules = (opts.surfaces ?? [[':scope', '--bg-2']]).map(([sel, tint]) =>
      `${sel} { background: linear-gradient(var(${tint}), var(${tint})), ${layers.join(', ')}; background-attachment: fixed; }`)
    out += `@scope (${scope}) {\n${rules.join('\n')}\n}\n`
  }
  const filtered = filterCSS(theme?.css ?? '', scope, id)
  return { css: out + filtered.css, dropped: filtered.dropped }
}

// A theme from the network, reduced to what is valid.
export function parseTheme(raw: unknown): Theme | null {
  if (!raw || typeof raw !== 'object') return null
  const t = raw as Record<string, unknown>
  const out: Theme = {}
  if (t.colors && typeof t.colors === 'object') {
    const colors: Theme['colors'] = {}
    for (const c of themeColors) {
      const v = (t.colors as Record<string, unknown>)[c]
      if (isColor(v)) colors[c] = v
    }
    if (Object.keys(colors).length) out.colors = colors
  }
  const g = t.gradient as Theme['gradient'] | undefined
  if (g && typeof g.angle === 'number' && Array.isArray(g.stops) && g.stops.length >= 2 && g.stops.length <= 4 && g.stops.every(isColor)) out.gradient = { angle: g.angle, stops: g.stops }
  if (typeof t.font === 'string' && t.font in themeFonts) out.font = t.font as ThemeFont
  if (typeof t.css === 'string' && t.css.trim()) out.css = t.css
  return Object.keys(out).length ? out : null
}
