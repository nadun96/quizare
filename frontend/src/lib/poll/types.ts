// Live polls (D-40): shapes of the poll API. Mirrors internal/poll in Go.
import type { TextFormat } from '../types';

export type PollType =
	| 'SINGLE' | 'MULTI' | 'MATCH' | 'BLANK_OPT' | 'BLANK_TEXT' | 'DRAG' | 'ESSAY'
	| 'SHORT_TEXT' | 'WORD_CLOUD' | 'NUMBER' | 'DATE' | 'TIME' | 'RATING' | 'SLIDER'
	| 'LIKERT' | 'MATRIX' | 'FILE' | 'AUDIO' | 'VIDEO' | 'CODE';

export type Choice = { id: string; text: string };

export type PollBody = {
	format?: TextFormat;
	options?: Choice[];
	display?: 'radio' | 'dropdown';
	max_choices?: number;
	left?: Choice[];
	right?: Choice[];
	zones?: Choice[];
	blanks?: string[];
	rows?: Choice[];
	columns?: Choice[];
	mode?: 'single' | 'multi' | 'text';
	scale?: string[];
	min?: number;
	max?: number;
	step?: number;
	min_label?: string;
	max_label?: string;
	unit?: string;
	from?: string;
	to?: string;
	points?: number;
	max_length?: number;
	max_words?: number;
	max_entries?: number;
	language?: string;
	accept?: ('image' | 'pdf' | 'document' | 'any')[];
	max_mb?: number;
	max_seconds?: number;
};

export type PollQuestion = {
	id: string;
	poll_id: string;
	position: number;
	type: PollType;
	text: string;
	body: PollBody;
	required: boolean;
	/** Teacher only (D-42); participants get keys in PublicPoll.keys once revealed. */
	key?: PollKey | null;
	points?: number;
	time_limit_sec?: number | null;
};

/** Answer key of a scorable question (D-42). */
export type PollKey = {
	correct?: string[];
	pairs?: Record<string, string>;
	blanks?: Record<string, string[]>;
	order?: string[];
	accepted?: string[];
	value?: number;
	tolerance?: number;
	case_sensitive?: boolean;
};
export type PollScore = { points: number; correct: boolean };
export type Rank = { rank: number; key: string; name: string; score: number; correct: number; answered: number; participant_id?: string; real_name?: string; nickname?: string; color?: number; members?: number; detail?: string };

// Groups (D-43).
export type GroupMode = 'off' | 'manual' | 'random' | 'categories' | 'self';
export type GroupAcceptance = 'all' | 'first' | 'captain' | 'best';
export type GroupCalc = 'sum' | 'average' | 'max' | 'min';
export type GroupInfo = { id: string; name: string; color: number; members: number };
export type GroupRank = { rank: number; id: string; name: string; color: number; members: number; score: number; answered: number };
export type GroupMember = { participant_id: string; name: string; nickname?: string; real_name?: string; captain: boolean; group_id?: string };
export type PollGroup = { id: string; poll_id: string; name: string; color: number; position: number; category_id?: string; members?: GroupMember[] };
export type GroupsView = { groups: PollGroup[]; ungrouped: GroupMember[] };
export type GroupAnswer = { by: string; value: PollAnswer };

export type FileRef = { id: string; name: string; size: number; content_type: string };

export type PollAnswer = {
	selected?: string[];
	pairs?: Record<string, string>;
	blanks?: Record<string, string>;
	order?: string[];
	text?: string;
	words?: string[];
	number?: number;
	date?: string;
	time?: string;
	rows?: Record<string, string>;
	multi?: Record<string, string[]>;
	cells?: Record<string, string>;
	file?: FileRef;
};

export type Identity = 'anonymous' | 'identified' | 'optional';
export type ShowResults = 'live' | 'after_answer' | 'presenter' | 'never';
export type LeaderboardMode = 'off' | 'presenter' | 'everyone';
export type ShowAnswers = 'never' | 'after_answer' | 'presenter' | 'after_close';
export type PollSettings = {
	identity: Identity;
	audience: 'anyone' | 'classroom';
	pacing: 'self' | 'presenter';
	show_results: ShowResults;
	allow_edit: boolean;
	scoring: boolean;
	speed_bonus: boolean;
	leaderboard: LeaderboardMode;
	show_answers: ShowAnswers;
	names: 'nickname' | 'name';
	groups: GroupMode;
	group_acceptance: GroupAcceptance;
	group_calc: GroupCalc;
};

export type Poll = PollSettings & {
	id: string;
	teacher_id: string;
	classroom_id: string | null;
	title: string;
	join_code: string;
	join_url: string;
	status: 'draft' | 'open' | 'closed';
	current_index: number;
	revealed: boolean;
	answers_revealed: boolean;
	question_started_at: string | null;
	created_at: string;
	participants: number;
	questions?: PollQuestion[];
};

export type WordCount = { word: string; count: number; hidden?: boolean };
export type TextItem = { participant_id?: string; name?: string; cell?: string; text: string; hidden?: boolean; at: number };
export type Bucket = { label: string; from: number; to: number; count: number };

export type PollResult = {
	question_id: string;
	type: PollType;
	responses: number;
	counts?: Record<string, number>;
	grid?: Record<string, Record<string, number>>;
	row_means?: Record<string, number>;
	ranks?: { id: string; avg_rank: number; first: number }[];
	words?: WordCount[];
	blank_words?: Record<string, WordCount[]>;
	texts?: TextItem[];
	stats?: { count: number; mean: number; median: number; min: number; max: number; buckets: Bucket[] };
	values?: { value: string; count: number }[];
	files?: { participant_id: string; name?: string; file: FileRef; hidden?: boolean; at: number }[];
};

export type PublicPoll = Omit<PollSettings, 'leaderboard'> & {
	type: 'poll' | 'update';
	server_time: number;
	code: string;
	title: string;
	status: 'draft' | 'open' | 'closed';
	current_index: number;
	revealed: boolean;
	total: number;
	questions: PollQuestion[];
	joined?: boolean;
	identified?: boolean;
	answers?: Record<string, PollAnswer>;
	results?: Record<string, PollResult>;
	leaderboard_mode: LeaderboardMode;
	answers_revealed: boolean;
	/** Server epoch ms when the current presenter-led question appeared. */
	question_started_at?: number;
	keys?: Record<string, PollKey>;
	scores?: Record<string, PollScore>;
	leaderboard?: Rank[];
	me?: Rank;
	me_key?: string;
	nickname?: string;
	group_mode: GroupMode;
	group_acceptance?: GroupAcceptance;
	groups?: GroupInfo[];
	group_leaderboard?: GroupRank[];
	my_group?: string;
	captain?: boolean;
	group_answers?: Record<string, GroupAnswer>;
	board_open?: boolean;
};

export type TeacherResults = { type: 'results'; server_time: number; poll: Poll; participants: number; results: Record<string, PollResult>; leaderboard?: Rank[]; group_leaderboard?: GroupRank[] };
