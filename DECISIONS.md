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
