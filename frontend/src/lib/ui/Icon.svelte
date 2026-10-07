<script lang="ts" module>
	// Small inline SVG icon set (stroke icons in the style of Lucide, ISC licence).
	// Inline SVG needs no font or extra request (NFR-09). Decorative by default.
	const PATHS = {
		check: 'M20 6 9 17l-5-5',
		'check-circle': 'M22 11.08V12a10 10 0 1 1-5.93-9.14M22 4 12 14.01l-3-3',
		alert: 'M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0zM12 9v4M12 17h.01',
		info: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM12 16v-4M12 8h.01',
		x: 'M18 6 6 18M6 6l12 12',
		clock: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM12 6v6l4 2',
		'wifi-off': 'M1 1l22 22M16.72 11.06A10.94 10.94 0 0 1 19 12.55M5 12.55a10.94 10.94 0 0 1 5.17-2.39M10.71 5.05A16 16 0 0 1 22.58 9M1.42 9a15.91 15.91 0 0 1 4.7-2.88M8.53 16.11a6 6 0 0 1 6.95 0M12 20h.01',
		cloud: 'M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z',
		sun: 'M12 17a5 5 0 1 0 0-10 5 5 0 0 0 0 10zM12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42',
		moon: 'M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z',
		monitor: 'M20 3H4a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V5a2 2 0 0 0-2-2zM8 21h8M12 17v4',
		type: 'M4 7V4h16v3M9 20h6M12 4v16',
		menu: 'M3 12h18M3 6h18M3 18h18',
		user: 'M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2M12 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8z',
		users: 'M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM23 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75',
		play: 'M5 3l14 9-14 9V3z',
		pause: 'M6 4h4v16H6zM14 4h4v16h-4z',
		flag: 'M4 15s1-1 4-1 5 2 8 2 4-1 4-1V3s-1 1-4 1-5-2-8-2-4 1-4 1zM4 22v-7',
		'arrow-right': 'M5 12h14M12 5l7 7-7 7',
		'arrow-left': 'M19 12H5M12 19l-7-7 7-7',
		plus: 'M12 5v14M5 12h14',
		book: 'M4 19.5A2.5 2.5 0 0 1 6.5 17H20M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z',
		qr: 'M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h3v3h-3zM20 14v.01M14 20h.01M17 17h4v4h-4',
		chart: 'M18 20V10M12 20V4M6 20v-6',
		eye: 'M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8zM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
		'eye-off': 'M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19M1 1l22 22',
		send: 'M22 2 11 13M22 2l-7 20-4-9-9-4 20-7z',
		logout: 'M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9',
		sliders: 'M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6',
		sparkles: 'M12 3l1.9 5.8L20 11l-6.1 2.2L12 19l-1.9-5.8L4 11l6.1-2.2zM19 3v4M21 5h-4',
		pencil: 'M12 20h9M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4z',
		minus: 'M5 12h14',
		square: 'M5 5h14v14H5z',
		circle: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z',
		eraser: 'M7 21h10M16.5 3.5l4 4L9 19H5l-2-2L14.5 5.5zM12 8l4 4',
		undo: 'M9 14 4 9l5-5M4 9h11a5 5 0 0 1 0 10h-3',
		download: 'M12 3v12M7 10l5 5 5-5M5 21h14',
		'arrow-up': 'M12 19V5M5 12l7-7 7 7',
		'arrow-down': 'M12 5v14M19 12l-7 7-7-7',
		'bold': 'M6 4h7a4 4 0 0 1 0 8H6zM6 12h8a4 4 0 0 1 0 8H6z',
		'italic': 'M19 4h-9M14 20H5M15 4 9 20',
		'underline': 'M6 4v6a6 6 0 0 0 12 0V4M4 20h16',
		'strike': 'M16 4H9a3 3 0 0 0-2.83 4M14 12a4 4 0 0 1 0 8H6M4 12h16',
		'code': 'm16 18 6-6-6-6M8 6l-6 6 6 6',
		'link': 'M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71',
		'list': 'M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01',
		'list-ordered': 'M10 6h11M10 12h11M10 18h11M4 6h1v4M4 10h2M6 18H4c0-1 2-2 2-3s-1-1.5-2-1',
		'quote': 'M7 7H4v6h3v3l-3 2M17 7h-3v6h3v3l-3 2',
		'braces': 'M8 3H7a2 2 0 0 0-2 2v5a2 2 0 0 1-2 2 2 2 0 0 1 2 2v5a2 2 0 0 0 2 2h1M16 21h1a2 2 0 0 0 2-2v-5a2 2 0 0 1 2-2 2 2 0 0 1-2-2V5a2 2 0 0 0-2-2h-1',
		'table': 'M3 3h18v18H3zM3 9h18M3 15h18M9 3v18M15 3v18',
		'sigma': 'M18 7V4H6l6 8-6 8h12v-3',
		'sigma-block': 'M3 3h18v18H3zM15 8H9l3 4-3 4h6',
		'redo': 'm15 14 5-5-5-5M20 9H9a5 5 0 0 0 0 10h3',
		'remove-format': 'M4 7V4h16v3M5 20h6M13 4 8 20M15 15l5 5M20 15l-5 5',
		'markdown': 'M3 5h18v14H3zM7 15V9l2 2 2-2v6M17 9v6M15 13l2 2 2-2',
		star: 'M12 3l2.7 5.6 6.1.9-4.4 4.3 1 6.1L12 17l-5.4 2.9 1-6.1-4.4-4.3 6.1-.9z',
		shuffle: 'M16 3h5v5M4 20 21 3M21 16v5h-5M15 15l6 6M4 4l5 5',
		trophy: 'M8 21h8M12 17v4M7 4h10v5a5 5 0 0 1-10 0zM17 5h3v2a3 3 0 0 1-3 3M7 5H4v2a3 3 0 0 0 3 3',
		brush: 'M9.06 11.9l8.07-8.06a2.85 2.85 0 1 1 4.03 4.03l-8.06 8.08M7.07 14.94c-1.66 0-3 1.35-3 3.02 0 1.33-2.5 1.52-2 2.02 1.08 1.1 2.49 2.02 4 2.02 2.2 0 4-1.8 4-4.04a3.01 3.01 0 0 0-3-3.02z',
		share: 'M4 12v8a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-8M16 6l-4-4-4 4M12 2v13'
	} as const;
	export type IconName = keyof typeof PATHS;
</script>

<script lang="ts">
	let { name, size = 18, label, class: cls = '' }: { name: IconName; size?: number; label?: string; class?: string } = $props();
</script>

<svg
	class="icon {cls}"
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	stroke="currentColor"
	stroke-width="2"
	stroke-linecap="round"
	stroke-linejoin="round"
	role={label ? 'img' : undefined}
	aria-label={label}
	aria-hidden={label ? undefined : 'true'}
	focusable="false"><path d={PATHS[name]} /></svg>

<style>
	.icon { display: inline-block; flex-shrink: 0; vertical-align: -0.15em; }
</style>
