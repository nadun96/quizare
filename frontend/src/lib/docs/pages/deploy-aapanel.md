# Deploying on aaPanel

This page runs the platform on a server managed with [aaPanel](https://www.aapanel.com), using aaPanel's own PostgreSQL for the data. It also covers updating to a new version later, built on your computer, without losing data and without signing anyone out.

```mermaid
flowchart LR
  B((Browsers)) -->|HTTPS 443| N[aaPanel Nginx<br/>your site + Let's Encrypt]
  N -->|127.0.0.1:8080| A[quiz app<br/>systemd service]
  A --> P[(aaPanel PostgreSQL<br/>database quiz)]
  PC[Your computer] -.->|package.ps1, upload| U[update.sh]
  U -.->|backup, switch, restart| A
```

Your computer builds everything: the server needs neither Go nor Node. You upload one file and run one command.

## What you need

- aaPanel on a Linux server with systemd (Ubuntu 20.04+, Debian 11+, AlmaLinux or Rocky 8+). 2 GB of memory or more.
- From aaPanel's **App Store**: **Nginx**, and **PostgreSQL 16** (or 15; 13 at least).
- A domain or subdomain whose DNS `A` record points at the server, for example `quiz.school.edu`.
- On your computer: this project, and the tools from [Getting started](/docs/getting-started) (Go and Node).

## 1. Create the database

In aaPanel, go to **Databases → PgSQL → Add database**:

| Field | Value |
|---|---|
| Database name | `quiz` |
| Username | `quiz` |
| Password | a long random one (aaPanel can generate it); note it down |
| Access | Local server (127.0.0.1) |

The app creates its tables itself on first start, and later changes them itself when a new version needs it.

## 2. Create the website

1. **Website → Add site.** Enter your domain, and choose **Static** (no PHP) and no database.
2. Open the site's **SSL** tab, choose **Let's Encrypt**, issue the certificate, and turn on **Force HTTPS**.

## 3. Build the package on your computer

In PowerShell, from the project folder:

```powershell
.\scripts\package.ps1
```

This makes `dist\quiz-<version>-linux-amd64.tar.gz` and a `.sha256` file beside it. For an ARM server, use `.\scripts\package.ps1 -Arch arm64`.

## 4. Upload and install

1. In aaPanel's **Files**, upload both files (the `.tar.gz` and the `.sha256`) to `/root`.
2. In aaPanel's **Terminal** (or over SSH as root), run:

   ```bash
   cd /root
   mkdir -p quiz-install && tar -xzf quiz-*-linux-amd64.tar.gz -C quiz-install
   bash quiz-install/install.sh quiz-*-linux-amd64.tar.gz
   ```

3. Answer its questions:
   - your **domain**;
   - the **database name, user and password** from step 1 (host `127.0.0.1`, port `5432`);
   - the **port for the app** (`8080`, unless something else on the server uses it).

**What `install.sh` sets up:**

| What | Where |
|---|---|
| A `quiz` system user | |
| The app | `/www/quiz-app` |
| The settings | `/www/quiz-app/config/quiz.env` |
| A new master key | `/www/quiz-app/config/kek.key` |
| A `quiz` service that starts at boot | systemd |

It then starts the app. Running it again later is safe: it keeps the settings and the key.

## 5. Point the website at the app

1. Open the site's **Config** in aaPanel (Website → your site → Config).
2. **Delete** these blocks. They would serve the app's scripts and images from the empty site folder, and every page would break:
   - `location ~ .*\.(gif|jpg|jpeg|png|bmp|swf)$ { … }`
   - `location ~ .*\.(js|css)?$ { … }`
   - any `location / { … }`

   Keep the SSL lines, and the `/.well-known` block that renews the certificate.
3. Paste the contents of `/www/quiz-app/nginx.conf` inside the `server { … }` block, below the SSL lines. To show it, run `cat /www/quiz-app/nginx.conf` in the terminal.
4. Save. aaPanel checks the config and reloads Nginx.

**What that config does:**
- sends pages, the API and live connections to the app;
- keeps WebSockets open, for live quizzes, polls and meetings;
- adds the same security headers as the other setups.

Open `https://your-domain`. You should see the login page.

## 6. Make the first admin, and keep the key safe

```bash
bash /www/quiz-app/admin.sh you@school.edu "Your Name"
```

It asks for the password. Then sign in at `https://your-domain/login`.

**Save `/www/quiz-app/config/kek.key` somewhere safe, off the server:** a password manager, or an encrypted USB stick. It encrypts stored LLM keys, sign-in provider secrets and Google Drive connections. A database backup restored without it loses those, though everything else is fine.

## Updating to a new version

Each time you've added features locally and released them (merged into `main`):

1. **On your computer:**
   ```powershell
   .\scripts\package.ps1
   ```
2. **Upload** the new `.tar.gz` and `.sha256` to `/root` with aaPanel's Files.
3. **On the server:**
   ```bash
   bash /www/quiz-app/update.sh /root/quiz-<version>-linux-amd64.tar.gz
   ```

`update.sh` keeps everything:

| Step | What it does |
|---|---|
| Checks the package | Refuses a file damaged on the way (`.sha256`) |
| Backs up the database | `pg_dump` to `/www/quiz-app/backups/before-<version>-<time>.dump`, keeping the last 10 |
| Unpacks beside the old one | Each version in `/www/quiz-app/releases/<version>`; the last 5 are kept |
| Switches and restarts | The new version applies its database changes itself as it starts |
| Checks it's up | If it doesn't answer within 90 seconds, switches back to the previous version and restarts that |

**What people notice:**

- **Signed-in people stay signed in.** Sessions are kept in the database, not in the app's memory.
- **Students in a live quiz** see "Reconnecting…" for a few seconds. Their answers are already saved, and the quiz carries on.
- **Live polls and meetings** reconnect by themselves. A meeting's whiteboard, though, is kept in memory and is cleared: update between lessons, or download the board first.
- **Nothing else is lost:** classrooms, quizzes, results, polls and uploaded files are all in the database.

To see which version is running:

```bash
cat /www/quiz-app/current/VERSION
```

## Going back

**To the previous version**, when the new one misbehaves but its database changes are harmless:

```bash
ls /www/quiz-app/releases
ln -sfn /www/quiz-app/releases/<older-version> /www/quiz-app/current
systemctl restart quiz
```

**And its data**, when the new version changed the database and the old one can't use it: restore the backup taken just before the update.

```bash
systemctl stop quiz
set -a; . /www/quiz-app/config/quiz.env; set +a
/www/server/pgsql/bin/pg_restore --clean --if-exists --no-owner --single-transaction \
  --dbname="$QP_DATABASE_URL" /www/quiz-app/backups/before-<version>-<time>.dump
ln -sfn /www/quiz-app/releases/<older-version> /www/quiz-app/current
systemctl start quiz
```

This removes anything written since the update.

## Backups

Keep more than the dumps taken before updates:

- **aaPanel → Cron → Backup Database:** a daily backup of the `quiz` PgSQL database, kept for a couple of weeks. Send it to remote storage too (aaPanel supports S3, FTP and others).
- **The app's own backups:** made from **Admin → Backups** in the platform, encrypted with a passphrase you choose, stored in `/www/quiz-app/data/backups`. See [Deployment](/docs/deployment#restoring-a-backup).
- **The master key and the settings:** `/www/quiz-app/config/`, once, off the server.

## Day to day

| Task | Command |
|---|---|
| Is it running? | `systemctl status quiz` |
| Its log, live | `journalctl -u quiz -f` |
| Restart | `systemctl restart quiz` |
| Change a setting | edit `/www/quiz-app/config/quiz.env`, then restart |

To send password-reset and verification emails:
1. Uncomment the `QP_SMTP_*` lines in `quiz.env` and fill them in.
2. Put the SMTP password in `/www/quiz-app/config/smtp_password` (mode 600).
3. Restart.

Never delete `/www/quiz-app/config` or the `quiz` database in aaPanel. Uninstalling the site in aaPanel doesn't touch either; it only removes the Nginx site.

## Tutoring

Tutoring (live video lessons) is off after `install.sh`: the admin console's Tutoring tab says "Tutoring isn't switched on". The package already holds what it needs: the tutoring service and LiveKit, its video server. `tutoring.sh` switches it on (D-91).

**Before you start:**
- **The server.** Video is sent from the server to every student. A lesson where 30 students watch a screen share uses about 40 Mbit/s of upload, so check your plan's bandwidth. LiveKit gets up to 1 GB of memory and the tutoring service 400 MB, on top of the app: 4 GB of memory and 2 CPUs or more.
- **A subdomain** for tutoring, for example `tutor.school.edu`, with its DNS `A` record pointing at the server.

**1. On the server**, once the app runs a version that has tutoring (v2.20.0 or later):

```bash
bash /www/quiz-app/tutoring.sh
```

It asks for the subdomain and four ports; the defaults suit most servers. Then it:

| What | Where |
|---|---|
| The secret the app and tutoring share, and LiveKit's key | `/www/quiz-app/config/tutoring.secret`, `livekit-keys.yaml` (root only) |
| Tutoring's settings, and LiveKit's | `/www/quiz-app/config/tutor.env`, `livekit.yaml` |
| Two services that start at boot | `quiz-livekit`, `quiz-tutoring` |
| Tutoring's address for the app | `QP_TUTORING_URL` in `quiz.env`, and the secret through `/etc/systemd/system/quiz.service.d/tutoring.conf` |
| Recordings, encrypted | `/www/quiz-app/data/recordings` |

Tutoring keeps its tables in the app's database, in their own `tutoring` schema: no second database. Running it again is safe; it keeps the secrets and settings.

**2. In aaPanel:**
1. **Website → Add site:** the tutoring subdomain, **Static**, no database. Its **SSL** tab: Let's Encrypt, **Force HTTPS**.
2. Its **Config:** delete aaPanel's static-file blocks (as in step 5 above), and paste `/www/quiz-app/nginx-tutor.conf` below the SSL lines. To show it: `cat /www/quiz-app/nginx-tutor.conf`.
3. **The app's site Config:** paste `/www/quiz-app/nginx.conf` again, replacing what you pasted before. It now lets the pages reach the tutoring address and play its recordings.
4. **Security** (aaPanel's firewall): open **TCP 7881** and **UDP 7882**, the video, or the ports you chose. If your provider has a firewall in its own dashboard, open them there too. Keep the other ports closed: only Nginx, on this server, uses them.

Then sign in as a teacher: the Tutoring page appears, and a classroom's sessions start from there.

**Updating** needs nothing more: `update.sh` restarts the tutoring service with each release, and LiveKit only when the release brings a new one. Restarting LiveKit drops the video of live lessons for a few seconds, so update between lessons.

| Task | Command |
|---|---|
| Are they running? | `systemctl status quiz-tutoring quiz-livekit` |
| Their logs | `journalctl -u quiz-tutoring -f`, `journalctl -u quiz-livekit -f` |
| Change a setting | edit `/www/quiz-app/config/tutor.env` (or `livekit.yaml`), then `systemctl restart quiz-tutoring` (or `quiz-livekit`) |

### The relay on port 443

If students sit behind school firewalls that let only HTTPS out, LiveKit's relay has to share port 443 with your websites. With aaPanel's nginx, its stream module splits 443 by name: `deploy/aapanel/nginx-turn.conf` holds the configuration and the steps (D-83). It moves **every** HTTPS site on the server to an internal port, so plan a quiet moment, and check the sites' `listen` lines again after changing their SSL settings in aaPanel, which rewrites them. This configuration hasn't been tried on aaPanel itself; the same split was tested with Caddy.

## Troubleshooting

| What you see | Why, and what to do |
|---|---|
| **502 Bad Gateway** | The app isn't running. Check `journalctl -u quiz -n 50`. The port in `nginx.conf` must match `QP_LISTEN` in `quiz.env` |
| **The login page loads without its style, or pages are blank** | aaPanel's static-file blocks are still in the site config (step 5). Remove them |
| **"cross-site request rejected" at login** | `QP_BASE_URL` in `quiz.env` must be exactly the address people use: `https://`, with or without `www.` as they type it. Fix it and restart |
| **Live quizzes keep "Reconnecting…"** | Nginx isn't passing WebSockets. Check the `Upgrade` and `Connection` lines from `nginx.conf` are in the site config |
| **Install or update says it can't sign in to the database** | The password or user is wrong, or the database's access isn't "Local server". Check in aaPanel → Databases → PgSQL |
| **The log says `permission denied for database quiz`** (or `for schema public`) | The database belongs to PostgreSQL's administrator, not the app's user (aaPanel does this; PostgreSQL 15 and newer also lock the `public` schema). Give it to the app's user, then restart: `sudo -u postgres /www/server/pgsql/bin/psql -d quiz -c 'ALTER DATABASE quiz OWNER TO quiz' -c 'ALTER SCHEMA public OWNER TO quiz'` (use your database and user names) |
| **The log says `master key file /run/credentials/quiz.service/kek must not be accessible to group/others`** | Versions before v2.19.2 refused the key as some systemd versions hand it over (readable by the service's group, in a folder only the service can open). Install v2.19.2 or later |
| **The log says `extension "pgcrypto" is not available` or `permission denied to create extension "pgcrypto"`** | Versions before v2.19.1 asked for it; aaPanel's PostgreSQL often doesn't ship it. Install v2.19.1 or later, which doesn't need it on PostgreSQL 13 and newer |
| **"pg_dump not found"** | Install PostgreSQL from aaPanel's App Store on this server. If the database is elsewhere, install the matching client tools and set `QP_PG_BIN` to their folder |
| **"Tutoring isn't switched on"** in the admin console | Run `tutoring.sh` (see Tutoring). If you did, `QP_TUTORING_URL` is missing from `quiz.env` or the app wasn't restarted: `systemctl restart quiz` |
| **`tutoring.sh` says the release has no tutoring service** | The running version predates it. Build a new package, run `update.sh` with it, then `tutoring.sh` |
| **After updating from v2.19.2 or older: `tutoring.sh: No such file`, then "no tutoring service"** | That update ran the old `update.sh`, which neither copies `tutoring.sh` nor marks the new programs executable. Once: `cp /www/quiz-app/current/tutoring.sh /www/quiz-app/ && chmod 755 /www/quiz-app/current/tutor /www/quiz-app/current/livekit-server`, then `tutoring.sh`. Later updates do it themselves |
| **`quiz-tutoring` keeps restarting with `address already in use`** | Another program has its port (8090 is common). Set a free one, for example `TUTOR_LISTEN="127.0.0.1:8091"` in `/www/quiz-app/config/tutor.env`, and run `tutoring.sh` again; it rewrites `nginx-tutor.conf`. Newer versions of `tutoring.sh` check the ports first |
| **Teachers see the Tutoring page, but sessions never connect** | The browser can't reach the video. Check TCP 7881 and UDP 7882 are open in aaPanel → Security **and** your provider's firewall, and that the tutoring site's config has the `location /rtc` block |
| **Tutoring pages say they can't reach the tutoring service** | The app's site still has its old config: paste `/www/quiz-app/nginx.conf` again (it allows the tutoring address). Check the tutoring subdomain has its SSL certificate |
| **An update went back to the old version** | It printed why; the log is in `journalctl -u quiz -n 80`. Your data is as it was, and there's a backup from just before |
