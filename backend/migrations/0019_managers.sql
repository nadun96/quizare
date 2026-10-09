-- Managers (PL-FR-10 to PL-FR-17, D-56). A manager is either a teacher who
-- keeps teaching (role 'teacher') or a manager-only account (role 'manager');
-- both have a row here with the admin features they were given.
ALTER TABLE auth.users DROP CONSTRAINT users_role_check;
ALTER TABLE auth.users ADD CONSTRAINT users_role_check CHECK (role IN ('student', 'teacher', 'manager', 'admin'));

CREATE TABLE auth.managers (
    user_id        uuid PRIMARY KEY REFERENCES auth.users (id) ON DELETE CASCADE,
    features       text[]      NOT NULL DEFAULT '{}',   -- nothing until the admin gives some (PL-NFR-07)
    granted_by     uuid        REFERENCES auth.users (id) ON DELETE SET NULL,
    granted_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    last_active_at timestamptz                          -- refreshed at most once a minute
);

-- Who acted as what: 'admin' or 'manager' for admin-console actions (PL-FR-15).
ALTER TABLE audit.events ADD COLUMN actor_role text;
CREATE INDEX events_actor_idx ON audit.events (actor_id, id);
