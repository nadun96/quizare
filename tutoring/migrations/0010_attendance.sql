-- Attendance reports (D-87, TS-FR-81 to TS-FR-83): whether a classroom's
-- teacher counts attendance in its results (TS-FR-82). The reports
-- themselves come from the sessions and visits already kept.

CREATE TABLE tutoring.classroom_settings (
    classroom_id          uuid PRIMARY KEY,
    attendance_in_results boolean     NOT NULL DEFAULT false,
    updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_classroom_started_idx ON tutoring.sessions (classroom_id, started_at) WHERE started_at IS NOT NULL;
