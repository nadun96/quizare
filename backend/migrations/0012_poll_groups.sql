-- Groups and group competitions in polls (V2-06, V2-07, D-43).
ALTER TABLE poll.polls
    ADD COLUMN groups           text NOT NULL DEFAULT 'off' CHECK (groups IN ('off', 'manual', 'random', 'categories', 'self')),
    ADD COLUMN group_acceptance text NOT NULL DEFAULT 'all' CHECK (group_acceptance IN ('all', 'first', 'captain', 'best')),
    ADD COLUMN group_calc       text NOT NULL DEFAULT 'sum' CHECK (group_calc IN ('sum', 'average', 'max', 'min'));

CREATE TABLE poll.groups (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    poll_id     uuid        NOT NULL REFERENCES poll.polls (id) ON DELETE CASCADE,
    name        text        NOT NULL,
    color       int         NOT NULL CHECK (color BETWEEN 1 AND 8), -- categorical palette slot
    position    int         NOT NULL DEFAULT 0,
    category_id uuid,                                               -- formed from this classroom category
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX poll_groups_name_key ON poll.groups (poll_id, lower(name));

ALTER TABLE poll.participants
    ADD COLUMN group_id uuid REFERENCES poll.groups (id) ON DELETE SET NULL,
    ADD COLUMN captain  boolean NOT NULL DEFAULT false;
CREATE UNIQUE INDEX poll_participants_captain_key ON poll.participants (group_id) WHERE captain;
CREATE INDEX poll_participants_group_idx ON poll.participants (group_id);

-- When an answer was first given ("the group's first answer"); updated_at moves on edits.
ALTER TABLE poll.responses
    ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();
