# Implementation decisions

Choices made where the Business Analysis (BA) document leaves room or poses
an open question. Each entry cites the BA/architecture source it relies on.
Open questions (BA §14, Q-01…Q-10) are answered provisionally here and can be
changed through configuration where noted.

| ID | Decision | Basis |
|----|----------|-------|
| D-01 | Ubuntu deploy target (systemd, Caddy). Code stays cross-platform; development and tests run on Windows. | Architecture §1.4; spreadsheet OS row conflicts with its own memory notes ("Linux OS") |
| D-02 | Students and teachers can log in before verifying their email. Verification is tracked (`email_verified`) and a reset link also verifies. | BO-2 (QR scan → first question < 90 s); FR-ACC-01 requires verification to exist, not to gate login |
| D-03 | Admin approval of teachers is a platform policy toggle, off by default. | FR-ACC-05 (S, "if institution requires"), Q-09 |
| D-04 | Deleting a user anonymises the account (email, name, password scrubbed; sessions revoked) instead of removing rows, so attempts and results survive. | NFR-04 deletion on request; BR-16 preserve results |
| D-05 | Admins are created only with `server create-admin`; there is no HTTP route. | BA §4 roles; least privilege |
| D-06 | Per-IP rate limits are generous (burst 300) because a classroom shares one NAT address; the per-account limit (10 burst, 1 per 30 s) stops guessing. | ADR-13 rate limiting; BO-2 |
| D-07 | Integration tests run against a real PostgreSQL 16 (embedded, or `QP_TEST_DATABASE_URL`). | ADR-03; avoids mock/prod divergence |
| D-08 | Q-06: by default a QR session join auto-enrols the student in the classroom (`auto_enrol_on_join=true`); teachers can turn it off per classroom, which then requires enrolling by classroom code first. | BR-02 ("if the classroom allows it"), BO-2 |
| D-09 | Once a student has entered their classroom student ID, only the teacher can change it. It is attached to every answer, so students must not be able to rewrite it mid-term. | BR-03, FR-CLS-05 |
| D-10 | Settings keys can be set only at the levels BA §7 lists for them, and this is validated on write. The admin's platform layer can set any key. Time extensions and pauses are not settings: they add up rather than override, so they are stored with the session and attempt. | BA §7 table and timing rules |
| D-11 | A classroom that has results can be archived but not deleted (the delete is refused); this is enforced once sessions exist. | BR-16 by analogy |
| D-12 | CSV import is partial: valid rows are imported and invalid rows are reported with row numbers and reasons. A question code that already exists in the quiz is a row error, so re-uploading only the failed rows works (UC-01 4a). | FR-QZ-05, AC-09 |
| D-13 | CSV extensions to the BA §10.3 template: `\|` escapes a literal pipe; DRAG uses `items` (ordering) or `items\|\|zones` with `item=zone` answers, mirroring MATCH; BLANK_TEXT accepts several answers per blank as repeated `1=a\|1=b`. Optional columns: `negative_marks`, `partial_credit`, `word_limit`, `case_sensitive`, `tolerance`. | BA §10.3 is silent on these |
| D-14 | ESSAY defaults to `llm` evaluation (BR-11 falls back to manual when there is no key). LLM marking is allowed only for ESSAY and BLANK_TEXT; answer-key marking is not allowed for ESSAY. | BA §10.1, §10.3 "defaults by type" |
| D-15 | Resource links are checked off the request path by a River job, with SSRF safeguards (public IPs only after DNS resolution, 5 MB limit, image content type, timeouts, no cookies). Broken links make Ready require `accept_warnings`. | FR-QZ-11, UC-01 6a, ADR-10 |
| D-16 | Live sessions will snapshot questions and keys at session start, so editing a quiz never changes a running session or past results. | ADR-14 |
