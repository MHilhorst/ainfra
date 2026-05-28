import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'ainfra',
  description: 'A package manager for your team\'s AI development setup.',
  cleanUrls: true,
  lastUpdated: true,
  appearance: 'dark',
  // Existing markdown uses GitHub-relative links (e.g. ../spec/manifest-schema)
  // that don't resolve to VitePress routes. Leaving this on until links are
  // migrated to site-relative paths.
  ignoreDeadLinks: true,

  srcExclude: [
    'brainstorms/**',
    'plans/**',
    'superpowers/**',
    'README.md',
  ],

  head: [
    ['link', { rel: 'icon', href: '/favicon.svg' }],
    ['meta', { name: 'theme-color', content: '#0a0a0a' }],
    ['meta', { property: 'og:title', content: 'ainfra' }],
    ['meta', { property: 'og:description', content: 'Keep your whole dev team\'s AI tooling in sync.' }],
  ],

  themeConfig: {
    nav: [
      { text: 'Guide', link: '/quickstart', activeMatch: '^/(quickstart|problem-space|design-philosophy)' },
      { text: 'Reference', link: '/reference/design', activeMatch: '^/reference/' },
      { text: 'Schemas', link: '/spec/manifest-schema', activeMatch: '^/spec/' },
      {
        text: 'v0.x',
        items: [
          { text: 'Changelog', link: 'https://github.com/MHilhorst/ainfra/releases' },
          { text: 'Releases', link: 'https://github.com/MHilhorst/ainfra/releases' },
        ],
      },
    ],

    sidebar: {
      '/': [
        {
          text: 'Guide',
          items: [
            { text: 'Introduction', link: '/' },
            { text: 'Quick Start', link: '/quickstart' },
            { text: 'Problem Space', link: '/problem-space' },
            { text: 'Design Philosophy', link: '/design-philosophy-references' },
          ],
        },
        {
          text: 'Reference',
          items: [
            { text: 'Design', link: '/reference/design' },
            { text: 'Validation', link: '/reference/validation' },
            { text: 'Assessment vs Real Config', link: '/reference/assessment-vs-real-config' },
            { text: 'ainfra vs sx', link: '/reference/sx-comparison' },
          ],
        },
        {
          text: 'Schemas',
          items: [
            { text: 'Manifest (ainfra.yaml)', link: '/spec/manifest-schema' },
            { text: 'Lockfile (ainfra.lock)', link: '/spec/lockfile-schema' },
          ],
        },
      ],
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/MHilhorst/ainfra' },
    ],

    editLink: {
      pattern: 'https://github.com/MHilhorst/ainfra/edit/main/docs/:path',
      text: 'Edit this page on GitHub',
    },

    search: {
      provider: 'local',
    },

    footer: {
      message: 'Released under the MIT License.',
      copyright: 'Copyright © 2026 Michael Hilhorst',
    },
  },
})
