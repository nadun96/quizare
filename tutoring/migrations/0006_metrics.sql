-- Monitoring (D-82, TS-NFR-41): one row a minute for all of tutoring
-- (session_id NULL) and one for each live session, kept 7 days. Bandwidth
-- for all of tutoring is measured from LiveKit when its metrics are
-- configured (measured), and otherwise, like every session's, estimated.

CREATE TABLE tutoring.metrics (
    at           timestamptz NOT NULL,
    session_id   uuid REFERENCES tutoring.sessions (id) ON DELETE CASCADE,
    up_mbps      real        NOT NULL,  -- sent by the media server, the minute's average
    down_mbps    real        NOT NULL,  -- received by it
    cpu          real,                  -- cores the media server used; NULL when not measured
    tracks       int         NOT NULL,  -- the minute's most
    participants int         NOT NULL,  -- the minute's most
    reconnects   int         NOT NULL,  -- during the minute
    measured     boolean     NOT NULL DEFAULT false
);
CREATE INDEX metrics_at_idx ON tutoring.metrics (at);
CREATE INDEX metrics_session_idx ON tutoring.metrics (session_id, at) WHERE session_id IS NOT NULL;
