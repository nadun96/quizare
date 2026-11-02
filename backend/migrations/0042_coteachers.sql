-- Co-teachers (D-121): a classroom's owner invites other teachers, who
-- teach it with them once they accept.
CREATE TABLE content.classroom_teachers (
    classroom_id uuid        NOT NULL REFERENCES content.classrooms (id) ON DELETE CASCADE,
    user_id      uuid        NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    status       text        NOT NULL DEFAULT 'invited' CHECK (status IN ('invited', 'active')),
    invited_by   uuid        REFERENCES auth.users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    accepted_at  timestamptz,
    PRIMARY KEY (classroom_id, user_id)
);
CREATE INDEX classroom_teachers_user_idx    ON content.classroom_teachers (user_id);
CREATE INDEX classroom_teachers_invited_idx ON content.classroom_teachers (invited_by);

-- Whether someone teaches a classroom: its owner, or a co-teacher who accepted.
-- Every "may this teacher work in this classroom" check asks this.
CREATE FUNCTION content.teaches(classroom uuid, who uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS (SELECT 1 FROM content.classrooms c WHERE c.id = classroom AND c.teacher_id = who)
        OR EXISTS (SELECT 1 FROM content.classroom_teachers t WHERE t.classroom_id = classroom AND t.user_id = who AND t.status = 'active')
$$;

-- The same for the classroom a topic is in.
CREATE FUNCTION content.teaches_topic(topic uuid, who uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT coalesce((SELECT content.teaches(m.classroom_id, who) FROM content.topics t JOIN content.modules m ON m.id = t.module_id WHERE t.id = topic), false)
$$;

-- The analysis warehouse (D-120) learns who co-teaches, so Explore shows a
-- co-teacher their classrooms; kept by the insight loader.
CREATE TABLE olap.coteachers (
    classroom_id uuid NOT NULL,
    user_id      uuid NOT NULL,
    PRIMARY KEY (classroom_id, user_id)
);
CREATE INDEX olap_coteachers_user_idx ON olap.coteachers (user_id);
