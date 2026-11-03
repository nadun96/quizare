-- Retries (D-131): a student may take a session's quiz again from the start
-- when the teacher lets them, or up to the session's "tries" setting. Each
-- try is its own attempt; only the newest is current (live, on the
-- dashboard), and the best valid result is the one that counts.
ALTER TABLE live.attempts
    ADD COLUMN try           integer     NOT NULL DEFAULT 1 CHECK (try >= 1),
    ADD COLUMN superseded_at timestamptz; -- a newer try replaced this one
ALTER TABLE live.attempts DROP CONSTRAINT attempts_session_id_user_id_key;
-- BR-01 now holds for the current try: one per student per session.
CREATE UNIQUE INDEX attempts_current_key ON live.attempts (session_id, user_id) WHERE superseded_at IS NULL;
CREATE UNIQUE INDEX attempts_try_key ON live.attempts (session_id, user_id, try);

-- The result that counts for each student in each session: their best
-- valid try (the latest of equals); with none valid, their latest try.
ALTER TABLE eval.results ADD COLUMN counts boolean NOT NULL DEFAULT true;
CREATE INDEX results_student_idx ON eval.results (session_id, user_id);

CREATE FUNCTION eval.recount(p_session uuid, p_user uuid) RETURNS void LANGUAGE sql AS $$
    UPDATE eval.results r SET counts = (r.attempt_id = best.attempt_id), updated_at = now() -- so the warehouse reloads it
    FROM (SELECT x.attempt_id FROM eval.results x JOIN live.attempts a ON a.id = x.attempt_id
          WHERE x.session_id = p_session AND x.user_id = p_user
          ORDER BY x.invalidated, x.pct DESC, a.try DESC LIMIT 1) best
    WHERE r.session_id = p_session AND r.user_id = p_user AND r.counts IS DISTINCT FROM (r.attempt_id = best.attempt_id);
$$;

CREATE FUNCTION eval.results_recount() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM eval.recount(OLD.session_id, OLD.user_id);
        RETURN OLD;
    END IF;
    PERFORM eval.recount(NEW.session_id, NEW.user_id);
    RETURN NEW;
END $$;

-- Only changes to a result's mark or validity move the count; setting
-- counts itself doesn't fire it again.
CREATE TRIGGER results_recount AFTER INSERT OR DELETE OR UPDATE OF pct, invalidated ON eval.results
    FOR EACH ROW EXECUTE FUNCTION eval.results_recount();

-- Whether an attempt is the student's try that counts in reports: the one
-- with the counting result; with no result yet, the current try, unless an
-- earlier try's result counts.
CREATE FUNCTION live.counted(p_attempt uuid) RETURNS boolean STABLE LANGUAGE sql AS $$
    SELECT CASE WHEN r.attempt_id IS NOT NULL THEN r.counts
                ELSE a.superseded_at IS NULL AND NOT EXISTS (
                    SELECT 1 FROM eval.results o WHERE o.session_id = a.session_id AND o.user_id = a.user_id AND o.counts)
           END
    FROM live.attempts a LEFT JOIN eval.results r ON r.attempt_id = a.id WHERE a.id = p_attempt
$$;
