// Precompress build output at build time so Caddy serves .br/.gz sidecars
// (`file_server { precompressed br gzip }`) with no per-request work (ADR-11).
import { readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { join, extname } from 'node:path';
import { brotliCompressSync, gzipSync, constants } from 'node:zlib';

const exts = new Set(['.html', '.js', '.css', '.json', '.svg', '.txt', '.map']);
function walk(dir) {
	for (const name of readdirSync(dir)) {
		const p = join(dir, name);
		if (statSync(p).isDirectory()) walk(p);
		else if (exts.has(extname(p)) && statSync(p).size > 512) {
			const data = readFileSync(p);
			writeFileSync(p + '.br', brotliCompressSync(data, { params: { [constants.BROTLI_PARAM_QUALITY]: 11 } }));
			writeFileSync(p + '.gz', gzipSync(data, { level: 9 }));
		}
	}
}
walk('build');
console.log('precompressed build/');
