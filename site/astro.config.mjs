// quarel.app: presentation page (src/pages/index.astro) and the wiki (Starlight, src/content/docs/wiki).
import { defineConfig, passthroughImageService } from 'astro/config'
import starlight from '@astrojs/starlight'

export default defineConfig({
  site: 'https://quarel.app',
  trailingSlash: 'always',
  redirects: { '/wiki/': '/wiki/decouvrir/presentation/' },
  // No image processing: sharp (LGPL libvips) is never used.
  image: { service: passthroughImageService() },
  integrations: [
    starlight({
      title: 'Quarel',
      logo: { src: './src/logo.svg' },
      favicon: '/favicon.svg',
      // French first; English later (add `en` here and translations under src/content/docs/en/).
      defaultLocale: 'root',
      locales: { root: { label: 'Français', lang: 'fr' } },
      social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/anlekg/quarel' }],
      editLink: { baseUrl: 'https://github.com/anlekg/quarel/edit/main/site/' },
      customCss: ['@fontsource/manrope/400.css', '@fontsource/manrope/600.css', '@fontsource/manrope/700.css', '@fontsource/space-grotesk/600.css', './src/styles/wiki.css'],
      sidebar: [
        { label: 'Découvrir', items: [{ autogenerate: { directory: 'wiki/decouvrir' } }] },
        { label: 'Utiliser Quarel', items: [{ autogenerate: { directory: 'wiki/utiliser' } }] },
        { label: 'Héberger', items: [{ autogenerate: { directory: 'wiki/heberger' } }] },
        { label: 'Développer', items: [{ autogenerate: { directory: 'wiki/developper' } }] },
      ],
      lastUpdated: true,
      pagination: true,
    }),
  ],
})
