// Developer documentation registry: navigation order and page sources.
import decisions from '../../../../DECISIONS.md?raw';

const files = import.meta.glob('./pages/*.md', { query: '?raw', import: 'default', eager: true }) as Record<string, string>;

export type DocPage = { slug: string; title: string; section: string; source: string };

// Sidebar order. Every file in ./pages must appear here (checked by a test).
export const NAV: { section: string; pages: [slug: string, title: string][] }[] = [
	{ section: 'Start here', pages: [['overview', 'Overview'], ['getting-started', 'Getting started'], ['glossary', 'Glossary']] },
	{
		section: 'System design',
		pages: [
			['architecture', 'Architecture'],
			['backend-modules', 'Backend modules'],
			['data-model', 'Data model'],
			['settings', 'Configuration hierarchy']
		]
	},
	{
		section: 'Core flows',
		pages: [
			['live-sessions', 'Live sessions & timing'],
			['proctoring', 'Proctoring'],
			['marking', 'Marking & feedback'],
			['analytics', 'Results & analytics'],
			['polls', 'Live polls'],
			['tutoring', 'Tutoring sessions']
		]
	},
	{ section: 'Frontend', pages: [['frontend', 'Frontend app'], ['ux-research', 'UX research & design rules']] },
	{ section: 'Interfaces', pages: [['api', 'HTTP API'], ['realtime', 'WebSocket protocol']] },
	{ section: 'Operations', pages: [['security', 'Security & privacy'], ['deployment', 'Deployment'], ['deploy-without-domain', 'Deploying without a domain'], ['testing', 'Testing']] },
	{ section: 'Contributing', pages: [['contributing', 'Contributing'], ['decisions', 'Decision log']] }
];

export function source(slug: string): string | undefined {
	if (slug === 'decisions') return decisions;
	return files[`./pages/${slug}.md`];
}

export const PAGES: DocPage[] = NAV.flatMap((s) => s.pages.map(([slug, title]) => ({ slug, title, section: s.section, source: source(slug) ?? '' })));

export function neighbours(slug: string): { prev?: DocPage; next?: DocPage } {
	const i = PAGES.findIndex((p) => p.slug === slug);
	return { prev: PAGES[i - 1], next: PAGES[i + 1] };
}

export const pageFiles = () => Object.keys(files).map((f) => f.replace('./pages/', '').replace('.md', ''));
