// /llms-full.txt: the whole wiki in one Markdown file for AI assistants.
import { getCollection } from 'astro:content'

export async function GET() {
  const pages = (await getCollection('docs')).sort((a, b) => a.id.localeCompare(b.id))
  const body = pages.map((p) => `# ${p.data.title}\n\nURL : https://quarel.app/${p.id}/\n\n${p.body ?? ''}`).join('\n\n---\n\n')
  return new Response('# Quarel — documentation\n\n' + body + '\n', { headers: { 'Content-Type': 'text/plain; charset=utf-8' } })
}
