<script setup lang="ts">
/**
 * Inline SVG sprite. Stroke-based, 24×24 grid, `currentColor` so icons inherit
 * text colour from their context. `IconName` is the single source of truth for
 * every icon name the app uses.
 */
export type IconName =
  | 'plus'
  | 'play'
  | 'pause'
  | 'refresh'
  | 'trash'
  | 'x'
  | 'check'
  | 'chevron-down'
  | 'chevron-left'
  | 'chevron-right'
  | 'chevron-up'
  | 'download'
  | 'folder'
  | 'file'
  | 'link'
  | 'user'
  | 'crown'
  | 'settings'
  | 'log-out'
  | 'search'
  | 'alert-triangle'
  | 'clock'
  | 'zap'
  | 'database'
  | 'wifi'
  | 'menu'
  | 'sun'
  | 'moon'
  | 'layers'
  | 'inbox'
  | 'eye'
  | 'eye-off'
  | 'copy'
  | 'external-link'
  | 'filter'
  | 'arrow-up'
  | 'arrow-down'
  | 'list'
  | 'loader'
  | 'terminal'
  | 'credit-card'
  | 'ticket'
  | 'power'
  | 'image'
  | 'video'
  | 'globe'
  | 'info'
  | 'arrow-left'

const PATHS: Record<IconName, string> = {
  plus: 'M12 5v14M5 12h14',
  play: 'M7 4.5v15l13-7.5-13-7.5z',
  pause: 'M8 4.5v15M16 4.5v15',
  refresh: 'M20 11a8 8 0 1 0-2.2 5.5M20 5.5V11h-5.5',
  trash: 'M4 7h16M9 7V5.2A1.2 1.2 0 0 1 10.2 4h3.6A1.2 1.2 0 0 1 15 5.2V7M6.5 7l.8 12.1A1.5 1.5 0 0 0 8.8 20.5h6.4a1.5 1.5 0 0 0 1.5-1.4L17.5 7M10 11v5.5M14 11v5.5',
  x: 'M6 6l12 12M18 6L6 18',
  check: 'M4.5 12.5l5 5 10-11',
  'chevron-down': 'M6 9.5l6 6 6-6',
  'chevron-left': 'M14.5 6l-6 6 6 6',
  'chevron-right': 'M9.5 6l6 6-6 6',
  'chevron-up': 'M6 14.5l6-6 6 6',
  download: 'M12 3.5v11M7.5 10.5l4.5 4.5 4.5-4.5M4 19.5h16',
  folder: 'M3.5 7.2A1.7 1.7 0 0 1 5.2 5.5h3.6l2 2.3h8A1.7 1.7 0 0 1 20.5 9.5v8.3a1.7 1.7 0 0 1-1.7 1.7H5.2a1.7 1.7 0 0 1-1.7-1.7V7.2z',
  file: 'M13.5 3.5H7.2A1.7 1.7 0 0 0 5.5 5.2v13.6a1.7 1.7 0 0 0 1.7 1.7h9.6a1.7 1.7 0 0 0 1.7-1.7V8.5l-5-5zM13.5 3.5v5h5',
  link: 'M10.5 13.5a3.5 3.5 0 0 0 5 0l2.5-2.5a3.5 3.5 0 0 0-5-5l-1.2 1.2M13.5 10.5a3.5 3.5 0 0 0-5 0L6 13a3.5 3.5 0 0 0 5 5l1.2-1.2',
  user: 'M12 12a3.75 3.75 0 1 0 0-7.5A3.75 3.75 0 0 0 12 12zM4.8 20a7.4 7.4 0 0 1 14.4 0',
  crown: 'M4 17.5h16M4.8 7l3.6 3L12 5.5l3.6 4.5L19.2 7l-1.4 8H6.2L4.8 7z',
  settings:
    'M12 15.2a3.2 3.2 0 1 0 0-6.4 3.2 3.2 0 0 0 0 6.4zM19.4 13.8l1.5 1.2-1.6 2.8-1.9-.5a6.9 6.9 0 0 1-1.7 1l-.4 2-3.2 0-.4-2a6.9 6.9 0 0 1-1.7-1l-1.9.5-1.6-2.8 1.5-1.2a7 7 0 0 1 0-2.1L6.1 8.5l1.6-2.8 1.9.5a6.9 6.9 0 0 1 1.7-1l.4-2h3.2l.4 2a6.9 6.9 0 0 1 1.7 1l1.9-.5 1.6 2.8-1.5 1.2a7 7 0 0 1 0 2.1z',
  'log-out': 'M14.5 8.5V6.2a1.7 1.7 0 0 0-1.7-1.7H5.7A1.7 1.7 0 0 0 4 6.2v11.6a1.7 1.7 0 0 0 1.7 1.7h7.1a1.7 1.7 0 0 0 1.7-1.7v-2.3M10 12h9.5M16.5 8.8L19.8 12l-3.3 3.2',
  search: 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM16.2 16.2L20.5 20.5',
  'alert-triangle': 'M12 4.2l8.2 14.2H3.8L12 4.2zM12 9.6v4.2M12 16.2h.01',
  clock: 'M12 20.5a8.5 8.5 0 1 0 0-17 8.5 8.5 0 0 0 0 17zM12 7.5V12l3 1.8',
  zap: 'M13.2 3.5L5.5 13.4h5L10 20.5l8-10h-5l.2-7z',
  database: 'M12 7.5c4.1 0 7.5-1.1 7.5-2.5S16.1 2.5 12 2.5 4.5 3.6 4.5 5s3.4 2.5 7.5 2.5zM4.5 5v14c0 1.4 3.4 2.5 7.5 2.5s7.5-1.1 7.5-2.5V5M4.5 12c0 1.4 3.4 2.5 7.5 2.5s7.5-1.1 7.5-2.5',
  wifi: 'M2.8 9.2a13 13 0 0 1 18.4 0M6 12.6a8.4 8.4 0 0 1 12 0M9.2 16a4 4 0 0 1 5.6 0M12 19.4h.01',
  menu: 'M4 7h16M4 12h16M4 17h16',
  sun: 'M12 16.5a4.5 4.5 0 1 0 0-9 4.5 4.5 0 0 0 0 9zM12 2.5v2.2M12 19.3v2.2M4.2 4.2l1.6 1.6M18.2 18.2l1.6 1.6M2.5 12h2.2M19.3 12h2.2M4.2 19.8l1.6-1.6M18.2 5.8l1.6-1.6',
  moon: 'M20 14.2A8.2 8.2 0 0 1 9.8 4a8.5 8.5 0 1 0 10.2 10.2z',
  layers: 'M12 3.5l8.5 4.3L12 12.1 3.5 7.8 12 3.5zM3.5 12.2l8.5 4.3 8.5-4.3M3.5 16.4l8.5 4.3 8.5-4.3',
  inbox: 'M4 13.5h4l1.4 2.6h5.2l1.4-2.6h4M4 13.5l2.4-7.4A1.7 1.7 0 0 1 8 4.8h8a1.7 1.7 0 0 1 1.6 1.3l2.4 7.4v4.8a1.7 1.7 0 0 1-1.7 1.7H5.7A1.7 1.7 0 0 1 4 18.3v-4.8z',
  eye: 'M2.5 12S6 6.2 12 6.2 21.5 12 21.5 12 18 17.8 12 17.8 2.5 12 2.5 12zM12 14.6a2.6 2.6 0 1 0 0-5.2 2.6 2.6 0 0 0 0 5.2z',
  'eye-off': 'M9.9 5.2A8.9 8.9 0 0 1 12 5c6 0 9.5 6 9.5 6a16 16 0 0 1-3 3.6M6.3 7.2A15.8 15.8 0 0 0 2.5 11s3.5 6 9.5 6a9 9 0 0 0 3.4-.7M4 4l16 16M10.2 10.3a2.6 2.6 0 0 0 3.5 3.6',
  copy: 'M9.5 9.5h9a1.5 1.5 0 0 1 1.5 1.5v9a1.5 1.5 0 0 1-1.5 1.5h-9A1.5 1.5 0 0 1 8 20v-9a1.5 1.5 0 0 1 1.5-1.5zM5.5 15.5A1.5 1.5 0 0 1 4 14V5a1.5 1.5 0 0 1 1.5-1.5h9A1.5 1.5 0 0 1 16 5v.5',
  'external-link': 'M14 4.5h5.5V10M19 5l-8 8M17 14.5v4A1.5 1.5 0 0 1 15.5 20h-10A1.5 1.5 0 0 1 4 18.5v-10A1.5 1.5 0 0 1 5.5 7h4',
  filter: 'M4 6h16l-6.2 7.3V19l-3.6 1.5v-7.2L4 6z',
  'arrow-up': 'M12 19.5v-15M6.5 10.5L12 5l5.5 5.5',
  'arrow-down': 'M12 4.5v15M6.5 13.5L12 19l5.5-5.5',
  list: 'M8.5 6.5h11M8.5 12h11M8.5 17.5h11M4.5 6.5h.01M4.5 12h.01M4.5 17.5h.01',
  loader: 'M12 3.5v3.2M12 17.3v3.2M20.5 12h-3.2M6.7 12H3.5M18 6l-2.3 2.3M8.3 15.7L6 18M18 18l-2.3-2.3M8.3 8.3L6 6',
  terminal: 'M5 7.5l4 4-4 4M11.5 16.5h7M4.5 4.5h15a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1h-15a1 1 0 0 1-1-1v-13a1 1 0 0 1 1-1z',
  'credit-card': 'M3.5 7.5h17v10a1.5 1.5 0 0 1-1.5 1.5H5a1.5 1.5 0 0 1-1.5-1.5v-10zM3.5 10.5h17M6.5 15h3',
  ticket: 'M4 8.5A1.5 1.5 0 0 1 5.5 7h13A1.5 1.5 0 0 1 20 8.5v1.2a2 2 0 0 0 0 3.6v1.2a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 4 14.5v-1.2a2 2 0 0 0 0-3.6V8.5zM10 7v10',
  power: 'M12 4v8M7.8 6.8a7 7 0 1 0 8.4 0',
  image: 'M4.5 6.2A1.7 1.7 0 0 1 6.2 4.5h11.6a1.7 1.7 0 0 1 1.7 1.7v11.6a1.7 1.7 0 0 1-1.7 1.7H6.2a1.7 1.7 0 0 1-1.7-1.7V6.2zM9 10.2a1.4 1.4 0 1 0 0-2.8 1.4 1.4 0 0 0 0 2.8zM4.9 16.4l4.4-4.2 3.3 3 2.6-2.3 4 3.7',
  video: 'M4 7.5A1.5 1.5 0 0 1 5.5 6h8A1.5 1.5 0 0 1 15 7.5v9a1.5 1.5 0 0 1-1.5 1.5h-8A1.5 1.5 0 0 1 4 16.5v-9zM15 10.5l5-2.8v8.6l-5-2.8',
  globe: 'M12 20.5a8.5 8.5 0 1 0 0-17 8.5 8.5 0 0 0 0 17zM3.6 9.5h16.8M3.6 14.5h16.8M12 3.5a13 13 0 0 1 0 17M12 3.5a13 13 0 0 0 0 17',
  info: 'M12 20.5a8.5 8.5 0 1 0 0-17 8.5 8.5 0 0 0 0 17zM12 10.5v5.2M12 7.8h.01',
  'arrow-left': 'M19.5 12h-15M10.5 6.5L5 12l5.5 5.5',
}

const props = withDefaults(
  defineProps<{
    name: IconName
    size?: number | string
    /** Filled glyphs (play, crown) read better without a stroke. */
    filled?: boolean
    strokeWidth?: number
  }>(),
  { size: 18, filled: false, strokeWidth: 1.7 },
)
</script>

<template>
  <svg
    :width="props.size"
    :height="props.size"
    viewBox="0 0 24 24"
    :fill="props.filled ? 'currentColor' : 'none'"
    stroke="currentColor"
    :stroke-width="props.filled ? 0 : props.strokeWidth"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
    class="shrink-0"
  >
    <path :d="PATHS[props.name]" />
  </svg>
</template>
