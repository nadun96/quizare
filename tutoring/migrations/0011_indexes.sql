-- Indexes for foreign keys looked up or cascaded through (D-119).
CREATE INDEX IF NOT EXISTS participants_visiting_idx ON tutoring.participants (visiting);
CREATE INDEX IF NOT EXISTS messages_group_idx        ON tutoring.messages (group_id);
CREATE INDEX IF NOT EXISTS group_members_group_idx   ON tutoring.group_members (group_id);
