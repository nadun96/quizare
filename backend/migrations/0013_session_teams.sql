-- Teams in live quiz sessions (V2-06, V2-07, D-44). Formation and scoring
-- rules are settings (team_mode, team_acceptance, team_calc).
CREATE TABLE live.teams (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id  uuid        NOT NULL REFERENCES live.sessions (id) ON DELETE CASCADE,
    name        text        NOT NULL,
    color       int         NOT NULL CHECK (color BETWEEN 1 AND 8), -- categorical palette slot
    position    int         NOT NULL DEFAULT 0,
    category_id uuid,                                               -- formed from this classroom category
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX live_teams_name_key ON live.teams (session_id, lower(name));

ALTER TABLE live.attempts
    ADD COLUMN team_id uuid REFERENCES live.teams (id) ON DELETE SET NULL,
    ADD COLUMN captain boolean NOT NULL DEFAULT false;
CREATE UNIQUE INDEX live_attempts_captain_key ON live.attempts (team_id) WHERE captain;
CREATE INDEX live_attempts_team_idx ON live.attempts (team_id);
