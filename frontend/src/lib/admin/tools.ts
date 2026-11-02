// The admin console's tools (D-118): what each is called, its icon and one
// line on what it's for, grouped as an office system groups its modules. The
// console opens on these tiles; which ones a person sees comes from
// consoleTabs, so a manager sees only what they were given.
import type { IconName } from '../ui/Icon.svelte';
import type { ConsoleTab } from '../staff';

export type ToolGroup = 'people' | 'activity' | 'messages' | 'settings' | 'system';
export type Tool = { key: ConsoleTab; label: string; icon: IconName; blurb: string; group: ToolGroup };

export const GROUPS: { key: ToolGroup; label: string }[] = [
	{ key: 'people', label: 'People and classes' },
	{ key: 'activity', label: 'Activity and reports' },
	{ key: 'messages', label: 'Messages' },
	{ key: 'settings', label: 'Settings and connections' },
	{ key: 'system', label: 'System' }
];

export const TOOLS: Record<ConsoleTab, Tool> = {
	users: { key: 'users', label: 'Users', icon: 'users', blurb: 'Accounts, roles, approvals and suspensions', group: 'people' },
	classes: { key: 'classes', label: 'Classes', icon: 'book', blurb: 'Every classroom, its teacher and students', group: 'people' },
	managers: { key: 'managers', label: 'Managers', icon: 'shield', blurb: 'Teachers given parts of the console', group: 'people' },
	live: { key: 'live', label: 'Live activity', icon: 'activity', blurb: 'Quizzes, polls and sessions running now', group: 'activity' },
	recordings: { key: 'recordings', label: 'Tutoring', icon: 'monitor', blurb: 'Bandwidth, monitoring and recordings', group: 'activity' },
	explore: { key: 'explore', label: 'Explore', icon: 'table', blurb: 'Any measure by date, classroom, teacher, topic or question type', group: 'activity' },
	usage: { key: 'usage', label: 'Usage', icon: 'chart', blurb: 'Counts of people, quizzes and attempts', group: 'activity' },
	email: { key: 'email', label: 'Email', icon: 'mail', blurb: 'Templates, sending, the mail server and queue', group: 'messages' },
	settings: { key: 'settings', label: 'Platform settings', icon: 'settings', blurb: 'Defaults for quizzes, approvals and meetings', group: 'settings' },
	signin: { key: 'signin', label: 'Sign-in', icon: 'key', blurb: 'School accounts: OpenID Connect and LDAP', group: 'settings' },
	ai: { key: 'ai', label: 'AI models', icon: 'sparkles', blurb: 'The school’s own Ollama models for marking', group: 'settings' },
	drive: { key: 'drive', label: 'Google Drive', icon: 'cloud', blurb: 'The Google app teachers connect through', group: 'settings' },
	storage: { key: 'storage', label: 'Storage', icon: 'hard-drive', blurb: 'Disk space and clean-ups', group: 'system' },
	backups: { key: 'backups', label: 'Backups', icon: 'database', blurb: 'Database backups, made and restored', group: 'system' },
	audit: { key: 'audit', label: 'Audit log', icon: 'list', blurb: 'Who changed what, and when', group: 'system' },
	capacity: { key: 'capacity', label: 'Capacity test', icon: 'trophy', blurb: 'How many students this server handles at once, with a report', group: 'system' }
};

/** The groups with the tools this person may open, in order; empty groups are left out. */
export function grouped(tabs: ConsoleTab[], q = ''): { key: ToolGroup; label: string; tools: Tool[] }[] {
	const words = q.trim().toLowerCase().split(/\s+/).filter(Boolean);
	const hit = (t: Tool) => words.every((w) => `${t.label} ${t.blurb}`.toLowerCase().includes(w));
	return GROUPS.map((g) => ({ ...g, tools: tabs.map((k) => TOOLS[k]).filter((t) => t.group === g.key && hit(t)) })).filter((g) => g.tools.length);
}

/** The tool an address asks for (?tool=…), if this person may open it. */
export function toolFrom(param: string | null, tabs: ConsoleTab[]): ConsoleTab | null {
	return param && (tabs as string[]).includes(param) ? (param as ConsoleTab) : null;
}
