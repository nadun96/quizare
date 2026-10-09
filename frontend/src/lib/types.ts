// Shapes of the Go API's JSON (see backend/internal/*).

export type QType = 'SINGLE' | 'MULTI' | 'MATCH' | 'BLANK_OPT' | 'BLANK_TEXT' | 'DRAG' | 'ESSAY';
export const QTYPES: QType[] = ['SINGLE', 'MULTI', 'MATCH', 'BLANK_OPT', 'BLANK_TEXT', 'DRAG', 'ESSAY'];
export const QTYPE_LABEL: Record<QType, string> = {
	SINGLE: 'Single answer',
	MULTI: 'Multiple answer',
	MATCH: 'Matching',
	BLANK_OPT: 'Fill in the blank (options)',
	BLANK_TEXT: 'Fill in the blank (own words)',
	DRAG: 'Drag and drop',
	ESSAY: 'Essay'
};

export type Choice = { id: string; text: string };
/** '' is plain text (CSV import, older questions); 'markdown' comes from the rich text editor (D-38). */
export type TextFormat = '' | 'markdown';
export type Body = { options?: Choice[]; left?: Choice[]; right?: Choice[]; zones?: Choice[]; blanks?: string[]; word_limit?: number; format?: TextFormat };
export type Key = {
	correct?: string[];
	pairs?: Record<string, string>;
	blanks?: Record<string, string[]>;
	order?: string[];
	case_sensitive?: boolean;
	tolerance?: number;
	model_answer?: string;
	rubric?: string;
};
export type Resource = { id: string; role: 'Q' | 'O' | 'F'; n: number; url: string; alt_text: string; source_url?: string; status?: string; message?: string };
export type StudentQuestion = { id: string; code: string; type: QType; text: string; body: Body; marks: number; resources: Resource[] };
export type Response = { selected?: string[]; pairs?: Record<string, string>; blanks?: Record<string, string>; order?: string[]; text?: string };

export type Overrides = Record<string, string | number | boolean | undefined>;
export type Effective = Record<string, string | number | boolean>;

export type Question = StudentQuestion & {
	quiz_id: string;
	position: number;
	key: Key;
	feedback: { correct?: string; incorrect?: string; options?: Record<string, string>; bands?: { min_pct: number; max_pct: number; text: string }[] };
	negative_marks: number;
	partial_credit: boolean;
	settings: Overrides;
};

export type StudentState = {
	type: 'state';
	team?: TeamInfo;
	captain?: boolean;
	server_time: number;
	attempt_id: string;
	session_id: string;
	session_title: string;
	quiz_title: string;
	session_status: string;
	state: 'waiting' | 'admitted' | 'in_progress' | 'paused' | 'submitted' | 'invalidated' | 'not_started';
	student_number: string | null;
	countdown_deadline: number | null;
	quiz_deadline: number | null;
	question_deadline: number | null;
	quiz_remaining_ms: number | null;
	question_remaining_ms: number | null;
	index: number;
	total: number;
	answered: boolean[] | null;
	one_way: boolean;
	question: StudentQuestion | null;
	answer: Response | null;
	violation_policy: string;
	warnings: number;
	allowed_warnings: number;
	blur_grace_ms: number;
	invalid_reason?: string;
};

export type Classroom = { id: string; name: string; description: string; join_code: string; settings: Overrides; effective?: Effective; archived: boolean };
export type Module = { id: string; classroom_id: string; name: string; position: number; settings: Overrides };
export type Topic = { id: string; module_id: string; name: string; position: number; settings: Overrides };
export type Quiz = {
	id: string;
	topic_id: string;
	title: string;
	description: string;
	status: 'draft' | 'ready' | 'archived';
	settings: Overrides;
	effective?: Effective;
	question_count: number;
	total_marks: number;
	warnings_accepted: boolean;
};
export type Session = { id: string; quiz_id: string; title: string; join_code: string; join_url: string; status: string; settings: Overrides; extension_sec: number; created_at: string; results_released_at?: string; started_at?: string };
export type DashboardRow = {
	attempt_id: string;
	name: string;
	user_id: string;
	avatar?: string;
	student_number: string | null;
	state: StudentState['state'];
	index: number;
	total: number;
	answered: number;
	warnings: number;
	violations: number;
	extension_sec: number;
	quiz_deadline: number | null;
	remaining_ms: number | null;
	/** Set while an admitted student counts down; admitted without it = waiting for the teacher (D-53). */
	countdown_deadline?: number | null;
	connected: boolean;
	invalid_reason?: string;
	team_id?: string;
	captain?: boolean;
};
export type Dashboard = { type: 'dashboard'; server_time: number; session: Session; counts: Record<string, number>; rows: DashboardRow[]; team_mode?: string; teams?: TeamInfo[]; start_mode?: 'countdown' | 'teacher' };

// Teams in live sessions (D-44).
export type TeamInfo = { id: string; name: string; color: number; position: number; category_id?: string; members: number };
export type TeamMember = { attempt_id: string; name: string; student_number: string | null; state: string; captain: boolean; team_id?: string };
export type TeamsView = { mode: string; acceptance: 'all' | 'first' | 'captain' | 'best'; calc: 'sum' | 'average' | 'max' | 'min'; teams: (TeamInfo & { member_list: TeamMember[] })[]; unassigned: TeamMember[] };
export type TeamStanding = { rank: number; id: string; name: string; color: number; members: number; finished: number; score: number; max_score: number; pct: number; complete: boolean };

/** Words shown for attempt states (FR-SS-10). */
export const STATE_LABEL: Record<string, string> = {
	waiting: 'Waiting',
	admitted: 'Admitted',
	in_progress: 'In progress',
	paused: 'Paused',
	submitted: 'Submitted',
	invalidated: 'Invalidated',
	not_started: 'Not started'
};
/** Shape icons, so attempt state never depends on colour alone (WCAG 1.4.1). */
export const STATE_ICON: Record<string, string> = {
	waiting: '◷',
	admitted: '→',
	in_progress: '▶',
	paused: '⏸',
	submitted: '✓',
	invalidated: '⚠',
	not_started: '○'
};
export const STATE_BADGE: Record<string, string> = { in_progress: 'ok', submitted: 'ok', paused: 'warn', admitted: 'warn', invalidated: 'danger' };
