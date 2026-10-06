-- Public live leaderboard links (V2-08, D-45): two more share-link scopes,
-- and nicknames as a way to identify poll participants.
ALTER TABLE analytics.share_links DROP CONSTRAINT share_links_scope_check;
ALTER TABLE analytics.share_links ADD CONSTRAINT share_links_scope_check
    CHECK (scope IN ('session', 'quiz', 'live_session', 'live_poll'));
ALTER TABLE analytics.share_links DROP CONSTRAINT share_links_identify_check;
ALTER TABLE analytics.share_links ADD CONSTRAINT share_links_identify_check
    CHECK (identify IN ('anonymous', 'student_id', 'nickname'));
