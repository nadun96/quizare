# Getting started

You need **Go 1.26+** and **Node 20.19+** (22 recommended). Docker is not required: backend tests run against an embedded PostgreSQL 16 that is downloaded on first use.

## Run the tests

```sh
# Terminal 1: one shared embedded PostgreSQL on port 54329 (leave it running)
cd backend
go run ./cmd/testdb

# Terminal 2: backend tests
cd backend
export QP_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:54329/postgres?sslmode=disable'
go test ./...

# Frontend checks and unit tests
cd frontend
npm install
npm run check
npm test
```

Without `QP_TEST_DATABASE_URL`, each test package starts its own embedded server. That works, but each package then takes about 30 s longer.

## Run the app locally

1. Create a database and a master key (the key is read from a file, never from an environment variable; ADR-09):

   ```sh
   openssl rand 32 > kek.key && chmod 0400 kek.key
   ```

2. Start the backend. It migrates the database on start:

   ```sh
   cd backend
   export QP_DATABASE_URL='postgres://postgres:postgres@localhost:54329/qp_dev?sslmode=disable'
   export QP_KEK_FILE=../kek.key
   export QP_BASE_URL=http://localhost:5173   # the origin the browser uses
   go run ./cmd/server
   ```

3. Start the frontend dev server, which proxies `/api`, `/ws` and `/beacon` to `127.0.0.1:8080`:

   ```sh
   cd frontend
   npm run dev
   ```

4. Create an admin (there is no HTTP route for this; D-05):

   ```sh
   echo 'a-strong-password' | go run ./cmd/server create-admin admin@school.edu "Admin"
   ```

Open <http://localhost:5173>, register a teacher and a student (use two browsers or a private window), and run a session end to end. Verification and reset emails are printed to the server log unless `QP_SMTP_ADDR` is set.

> `QP_BASE_URL` must match the origin in the browser's address bar. The server rejects state-changing requests and WebSocket upgrades whose `Origin` differs (see [Security](security.md)).

## Configuration

| Variable | Default | Meaning |
|----------|---------|---------|
| `QP_DATABASE_URL` | (required) | PostgreSQL URL |
| `QP_KEK_FILE` | `$CREDENTIALS_DIRECTORY/kek` | 32-byte master key file (raw, hex or base64); mode 0400 on Unix |
| `QP_BASE_URL` | `http://localhost:8080` | Public origin: QR links and Origin/CSRF checks |
| `QP_LISTEN` | `127.0.0.1:8080` | Listen address |
| `QP_DB_MAX_CONNS` | `15` | pgx pool size |
| `QP_ARGON2_WORKERS` | `2` | Concurrent password hashes |
| `QP_STATIC_DIR` | | Serve a frontend build from Go (no Caddy) |
| `QP_API_DOCS` | on | `0` stops serving `/api/docs` |
| `QP_SMTP_ADDR`, `QP_SMTP_FROM`, `QP_SMTP_USER`, `QP_SMTP_PASSWORD_FILE` | | SMTP relay; the password is read from a file |
| `QP_DEV` | | `1` for development conveniences |

## Single-binary mode

To try a production-like setup without Caddy, build the frontend and let Go serve it:

```sh
cd frontend && npm run build
cd ../backend && QP_STATIC_DIR=../frontend/build QP_BASE_URL=http://localhost:8080 go run ./cmd/server
```

## Your first change

1. Branch from `dev`: `git checkout -b feature/<name> dev`.
2. Write the test first, next to the module you are changing (see [Testing](testing.md)).
3. Run `gofmt`, `go vet ./...`, `go test ./...`, `npm run check` and `npm test`.
4. If the change adds or alters an HTTP route, update `backend/internal/apidocs/openapi.yaml`; a test fails until you do.
5. If you make a choice the BA document leaves open, add a row to `DECISIONS.md`.
6. Merge back into `dev` with `--no-ff`. See [Contributing](contributing.md).
