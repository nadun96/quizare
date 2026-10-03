import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vitest/config';

const backend = process.env.QP_BACKEND ?? 'http://127.0.0.1:8080';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		// Same-origin in development so the __Host- cookie and Origin checks work.
		proxy: {
			'/api': { target: backend, changeOrigin: false },
			'/beacon': { target: backend, changeOrigin: false },
			'/ws': { target: backend, ws: true, changeOrigin: false }
		}
	},
	test: { include: ['src/**/*.test.ts'], environment: 'jsdom' }
});
