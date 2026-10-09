# Security & privacy

ADR-09, ADR-10, ADR-13 and ADR-16, with NFR-01…07 and BR-10/12/13/14. This page lists what the code does; treat it as the checklist when you change related code.

## Authentication and sessions

- **Passwords**: Argon2id with m = 19 MiB, t = 2, p = 1, in a **bounded pool** (2 workers, queue of 200, 10 s wait, then `503 Retry-After`). Without the cap, a class logging in at once would need about 1.9 GB.
- **Login timing**: an unknown email still runs one hash, so response time does not reveal which accounts exist. Password-reset requests always return 202.
- **Sessions**: a 256-bit random token in `__Host-sid` with `Secure; HttpOnly; SameSite=Lax; Path=/` and no Domain. Only its SHA-256 is stored. The token rotates on login, and suspension or a password reset revokes every session. Users are cached for 30 s; suspend, delete and reset evict the cache immediately.
- **Timeouts**, enforced server-side:

  | Role | Idle | Absolute |
  |------|------|----------|
  | Student | 7 days | 30 days |
  | Teacher | 24 h | 14 days |
  | Admin | 30 min | 12 h |
  | Manager (teacher-managers too) | 30 min | 12 h |

- **Why Lax, not Strict**: so the session survives opening a QR link from a camera app. CSRF is covered by the custom header plus the Origin check (`httpx.SameOrigin`), and WebSocket upgrades check Origin.
- **Rate limits** (token bucket): 300 burst per IP (a class shares one NAT address) and 10 per email, refilling one every 30 s.
- **No tokens in `localStorage`.** Only the offline answer queue lives there.

## Changing a password and profile pictures (D-49)

- **Changing a password** (`POST /api/auth/me/password`) needs the current one, follows the same length rule as registration, and is rate-limited per account like logins, so the current password can't be guessed through it. Every other session of the account is deleted; the one making the change stays. The owner is emailed, with a link to reset the password if it wasn't them, and the change is audited.
- **Profile pictures** (`PUT /api/auth/me/avatar`): at most 512 KB, PNG, JPEG, WebP or GIF, and under 8000 × 8000 pixels; the dimensions are read before decoding so oversized images are refused early. Every picture is decoded and re-encoded as a 256 × 256 JPEG, so nothing but pixels is kept: no EXIF (GPS position, camera), no comments, no file that is also something else. SVG is never accepted.
- Pictures are served only to logged-in users who may see them: the person, admins, and anyone who shares a classroom with them (a teacher and their students); anyone else gets 404, so the endpoint can't tell whether a picture exists. Responses are `image/jpeg` with `nosniff`, `Content-Security-Policy: sandbox` and private caching; the URL carries the picture's version, so a changed picture is never served stale. Anonymous poll participants have no account, so no picture is ever linked to them.

## Authorisation

Every query is scoped to the owner (`teacher_id`, or the attempt's `user_id`), and foreign ids return 404 (BR-14). Roles are enforced per route group with `RequireRole`. The admin console lets in admins and managers (`RequireStaff`), and each admin route also checks its feature with `RequireFeature` on every request: a manager without it gets 404 and the attempt goes to the audit log. Managers can act only on teachers and students, never on admins or other managers, and only admins make or change managers (D-56). Admins can't read quiz content, answers or keys; the admin module only counts rows.

## Answer keys

Students receive `StudentView` questions only: no key, feedback or rubric (BR-12, NFR-07). Keys reach a student only in `StudentResult` after release, and only when `results_show_correct` is on. Tests assert that preview, attempt state and the WebSocket payloads contain no key fields.

## Teacher API keys (ADR-09)

- **Envelope encryption.** AES-256-GCM with a fresh DEK per key, AAD `teacher_id|provider|version`. The DEK is wrapped with the KEK.
- **The master key comes from a file.** Set `QP_KEK_FILE`, or use systemd `LoadCredential=kek:/etc/quiz/kek.key`. It is never read from an environment variable, because those leak through `/proc/self/environ`. `LoadKEK` refuses files readable by group or others.
- **The API never returns a key.** Only list (last 4 characters), add, update, test and delete exist (AC-11). Plaintext lives only for the duration of one provider call and is zeroed afterwards.
- **Logging.** Never log request headers or API keys. Provider error messages are truncated before they are stored.
- **Limits, stated honestly.** "Admins cannot read keys" holds at the application layer. A root operator could read process memory and the key file; a KMS or HSM would remove that, but is out of scope on one box.
- **Back up the KEK offline.** Losing it means every teacher must re-enter their keys.

## Data sent to LLM providers (ADR-16, NFR-05)

- **Sent:** the question, model answer, rubric and the student's answer under a pseudonym. Emails and phone numbers are scrubbed when the request is built.
- **Never sent:** names, emails or classroom student IDs.
- **Prompt injection:** the answer sits in a delimited block, and output must match a JSON schema. The score is clamped, and suspicious results are flagged. The teacher's override is final.
- **Consent:** teachers tick a consent notice before adding a key. Each teacher's own provider account and its retention terms apply.

## Outbound fetches (SSRF, ADR-10)

Resource link checks use `imageurl.Checker`. The dialer refuses non-public IPs (loopback, private, link-local, CGNAT, multicast, unspecified) **after DNS resolution**, re-checks on every redirect (at most 5), and never uses an environment proxy. Downloads are capped at 5 MB, the content type must be `image/*`, timeouts apply, and no cookies are sent. Only `http` and `https` URLs without credentials are accepted.

## Browser hardening

- **Caddy**:
  - HSTS, `nosniff`, `Referrer-Policy: same-origin`, `frame-ancestors 'none'`.
  - A strict CSP: `default-src 'self'`, `img-src 'self' https: data:` for teacher image URLs, `connect-src 'self' wss:`, `object-src 'none'`.
- **Inline scripts**: SvelteKit's hash-based `<meta>` CSP blocks every inline script except its own hashed boot script.
- **Go**: adds `nosniff`, `X-Frame-Options: DENY` and `Referrer-Policy` to every response.
- **Public results pages**: `X-Robots-Tag: noindex` and `Cache-Control: no-store`.

## Input handling

- SQL is always parameterised (pgx).
- JSON decoding rejects unknown fields; bodies are limited to 1 MiB.
- CSV import: 2 MB, 1000 rows, UTF-8 only (a BOM is tolerated), strict per-row validation.
- CSV export neutralises formula injection (quiz analytics and poll exports).
- Rich text (question text and feedback) is stored as Markdown and rendered with raw HTML escaped, only `http(s):`/`mailto:` links, no images, and a DOMPurify allowlist (D-38).
- Poll uploads are read with a hard size cap per question type before anything is stored.

## Polls (D-40)

- **Anonymous participants** get a random 256-bit token at join. Only its SHA-256 is stored; the browser sends it as `X-Poll-Token`. It can only answer as that participant in that poll. An anonymous poll never stores a user id, even for a logged-in visitor.
- **Identified polls** use the normal session cookie; classroom-only polls also require an active enrolment.
- **Participants never see** names, participant ids, uploaded files or moderated answers. The participant WebSocket is read-only and carries no identity.
- **Uploads** (file, audio, video) are typed by sniffing the bytes, not by the browser's claim, and limited to 5 MB, 3 MB and 12 MB respectively, with 200 MB per poll. Only the poll owner can download them. Downloads are sent with `Content-Security-Policy: sandbox`, `nosniff` and `no-store`, and only images, audio and video are shown inline.
- Joins are rate-limited per IP address (150 at once, then 4 a second, so a class behind one school router can join together), and answers and uploads per participant.
- **Client address.** `X-Forwarded-For` is believed only when the direct peer is loopback or listed in `QP_TRUSTED_PROXIES`, and the client is the rightmost address in it that isn't a trusted proxy, so a client can't choose its own address (D-51).

## Privacy (NFR-04, BR-13)

- **Users can download their own data** from `GET /api/my/data`.
- **Deleting an account anonymises it** rather than removing rows, so a teacher's results stay intact.
- **Public links never show names or emails.** Students appear as "Student N", or by classroom student ID if the teacher chooses. Individual answers appear only when explicitly enabled. Links are hashed, revocable and can expire.
- **Audit log.** It records admin and manager actions (with the role the person acted in), refused manager attempts, mark overrides, reinstatements, key changes, releases and share links.

**Not built yet:** a scheduled retention job that anonymises old attempts (D-34).
