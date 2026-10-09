// Labels and levels for the hierarchical settings (BA §7, backend/internal/settings).

export type Level = 'teacher' | 'classroom' | 'module' | 'topic' | 'quiz' | 'question' | 'session' | 'student' | 'platform';
export type Meta = { key: string; label: string; kind: 'int' | 'bool' | 'enum' | 'text'; levels: Level[]; options?: [string, string][]; unit?: string; help?: string };

export const SETTINGS: Meta[] = [
	{ key: 'countdown_seconds', label: 'Pre-start countdown', kind: 'int', unit: 's', levels: ['teacher', 'classroom', 'quiz', 'session'] },
	{ key: 'question_time_limit_sec', label: 'Time limit per question', kind: 'int', unit: 's', levels: ['quiz', 'question'], help: '0 = no limit' },
	{ key: 'quiz_time_limit_sec', label: 'Quiz time limit', kind: 'int', unit: 's', levels: ['topic', 'quiz', 'session', 'student'], help: '0 = no limit' },
	{ key: 'admission_mode', label: 'Admission', kind: 'enum', levels: ['classroom', 'session'], options: [['manual', 'Teacher admits'], ['auto', 'Admit automatically']] },
	{ key: 'start_mode', label: 'Quiz start', kind: 'enum', levels: ['teacher', 'classroom', 'quiz', 'session'], options: [['countdown', 'Countdown starts on admission'], ['teacher', 'Teacher presses Start']] },
	{ key: 'evaluation_method', label: 'Marking method', kind: 'enum', levels: ['teacher', 'quiz', 'question'], options: [['key', 'Answer key'], ['llm', 'LLM'], ['manual', 'Manual']] },
	{ key: 'feedback_mode', label: 'Feedback', kind: 'enum', levels: ['teacher', 'quiz', 'question'], options: [['none', 'None'], ['predefined', 'Predefined'], ['ai', 'AI'], ['both', 'Predefined + AI']] },
	{ key: 'llm_model', label: 'LLM model override', kind: 'text', levels: ['teacher', 'quiz', 'question'] },
	{ key: 'violation_policy', label: 'When a student leaves the quiz', kind: 'enum', levels: ['classroom', 'quiz', 'session'], options: [['invalidate', 'Invalidate attempt'], ['warn_then_invalidate', 'Warn, then invalidate'], ['log_only', 'Only log it']] },
	{ key: 'allowed_warnings', label: 'Warnings before invalidating', kind: 'int', levels: ['classroom', 'quiz', 'session'] },
	{ key: 'blur_grace_ms', label: 'Ignore focus loss shorter than', kind: 'int', unit: 'ms', levels: ['classroom', 'quiz', 'session'] },
	{ key: 'disconnect_grace_sec', label: 'Allowed reconnect time', kind: 'int', unit: 's', levels: ['classroom', 'quiz', 'session'] },
	{ key: 'question_order', label: 'Question order', kind: 'enum', levels: ['quiz', 'session'], options: [['fixed', 'Fixed'], ['shuffled', 'Shuffled']] },
	{ key: 'option_order', label: 'Answer option order', kind: 'enum', levels: ['quiz', 'question'], options: [['fixed', 'Fixed'], ['shuffled', 'Shuffled']] },
	{ key: 'one_way_navigation', label: 'One-way navigation (no going back)', kind: 'bool', levels: ['quiz'] },
	{ key: 'results_visibility', label: 'Results visibility', kind: 'enum', levels: ['quiz', 'session'], options: [['private', 'Private'], ['public', 'Public page']] },
	{ key: 'results_view', label: 'Public results show', kind: 'enum', levels: ['quiz', 'session'], options: [['individual', 'Individual results'], ['question_pct', 'Question % correct'], ['pass_rate', 'Class pass rate']] },
	{ key: 'results_release', label: 'Release results', kind: 'enum', levels: ['quiz', 'session'], options: [['immediate', 'Immediately'], ['on_session_end', 'When the session ends'], ['manual', 'When I release them']] },
	{ key: 'results_show_answers', label: 'Students see their answers', kind: 'bool', levels: ['quiz', 'session'] },
	{ key: 'results_show_correct', label: 'Students see correct answers', kind: 'bool', levels: ['quiz', 'session'] },
	{ key: 'results_show_feedback', label: 'Students see feedback', kind: 'bool', levels: ['quiz', 'session'] },
	{ key: 'team_mode', label: 'Teams', kind: 'enum', levels: ['quiz', 'session'], options: [['off', 'No teams'], ['manual', 'I put students in teams'], ['random', 'At random'], ['categories', 'From classroom categories'], ['self', 'Students choose']] },
	{ key: 'team_acceptance', label: 'Team marks count', kind: 'enum', levels: ['quiz', 'session'], options: [['all', "Every member's marks"], ['first', 'First answer per question'], ['captain', "Captain's marks"], ['best', 'Best mark per question']] },
	{ key: 'team_calc', label: 'Combine members by', kind: 'enum', levels: ['quiz', 'session'], options: [['sum', 'Total'], ['average', 'Average'], ['max', 'Highest'], ['min', 'Lowest']] },
	{ key: 'pass_mark_pct', label: 'Pass mark', kind: 'int', unit: '%', levels: ['classroom', 'quiz'] },
	{ key: 'student_id_required', label: 'Require a classroom student ID', kind: 'bool', levels: ['classroom'] },
	{ key: 'enrolment_approval', label: 'Approve enrolments', kind: 'bool', levels: ['classroom'] },
	{ key: 'auto_enrol_on_join', label: 'Joining a session enrols the student', kind: 'bool', levels: ['classroom'] }
];

export const forLevel = (level: Level) => SETTINGS.filter((m) => level === 'platform' || m.levels.includes(level));
