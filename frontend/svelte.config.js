import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

// ADR-11: static SPA, no Node server in production. Caddy serves build/ and
// falls back to index.html for client-side routes.
export default {
	preprocess: vitePreprocess(),
	kit: {
		adapter: adapter({ pages: 'build', assets: 'build', fallback: 'index.html', precompress: false, strict: true })
	}
};
