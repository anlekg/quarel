// /llms.txt: table of contents of the wiki for AI assistants (llmstxt.org).
import { getCollection } from 'astro:content'

export async function GET() {
  const pages = (await getCollection('docs')).sort((a, b) => a.id.localeCompare(b.id))
  const lines = [
    '# Quarel',
    '',
    '> Alternative libre et auto-hébergeable à Discord : serveurs communautaires chez leurs hébergeurs, services d’identité pour les comptes, messages privés et appels chiffrés de bout en bout. Documentation en français.',
    '',
    'Toute la documentation en un fichier : https://quarel.app/llms-full.txt — serveur MCP : https://quarel.app/mcp',
    '',
    '## Wiki',
    '',
    ...pages.map((p) => `- [${p.data.title}](https://quarel.app/${p.id}/): ${p.data.description ?? ''}`),
    '',
  ]
  return new Response(lines.join('\n'), { headers: { 'Content-Type': 'text/plain; charset=utf-8' } })
}
