-- Peer-to-peer meetings (D-69): small calls whose audio and video go
-- directly between browsers. The server keeps the meeting, who joined, and
-- relays only the connection set-up messages.
CREATE SCHEMA meet;

CREATE TABLE meet.meetings (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    host_id      uuid        NOT NULL REFERENCES auth.users (id),
    title        text        NOT NULL,
    code         text        NOT NULL UNIQUE,
    -- Only this classroom's students (and the host) may join; NULL = anyone signed in with the link.
    classroom_id uuid        REFERENCES content.classrooms (id) ON DELETE SET NULL,
    max_people   int         NOT NULL DEFAULT 6 CHECK (max_people BETWEEN 2 AND 8),
    status       text        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'ended')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    ended_at     timestamptz
);
CREATE INDEX meetings_host_idx ON meet.meetings (host_id, created_at DESC);

-- Who joined, for the host: first and last time each person was in.
CREATE TABLE meet.attendance (
    meeting_id   uuid        NOT NULL REFERENCES meet.meetings (id) ON DELETE CASCADE,
    user_id      uuid        NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    first_joined timestamptz NOT NULL DEFAULT now(),
    last_seen    timestamptz NOT NULL DEFAULT now(),
    joins        int         NOT NULL DEFAULT 1,
    PRIMARY KEY (meeting_id, user_id)
);
