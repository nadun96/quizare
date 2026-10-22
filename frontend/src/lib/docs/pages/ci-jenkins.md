# CI/CD with Jenkins

This page sets up a build, test and deploy pipeline for the platform using only open-source tools. Jenkins runs the pipeline, Podman runs PostgreSQL for the tests and builds the container image, and SSH and systemd install the build on the servers. It replaces the GitHub Actions workflow in `.github/workflows/ci.yml`, which runs the same checks; keep both or remove that file.

Every push is checked. `test` deploys to a staging server; `main` deploys to production after someone clicks **Deploy**. That matches the branch flow: `feature/*` → `dev` → `test` → `main` (see [Contributing](/docs/contributing)).

## The tools

| Tool | Licence | What it does here |
|---|---|---|
| [Jenkins](https://www.jenkins.io) LTS | MIT | Runs the pipeline |
| [Forgejo](https://forgejo.org) or [Gitea](https://about.gitea.com) (optional) | GPL-3.0 / MIT | Hosts the repository yourself; GitHub works too |
| Go (version in `backend/go.mod`) | BSD-3-Clause | Builds and tests the server and the tutoring service |
| Node.js 22 | MIT | Builds and tests the frontend |
| [Podman](https://podman.io) | Apache-2.0 | PostgreSQL for the tests, and the container image |
| PostgreSQL 16 | PostgreSQL | The test database |
| [govulncheck](https://go.dev/doc/security/vuln/) | BSD-3-Clause | Known vulnerabilities in Go code that's actually called |
| [Redocly CLI](https://redocly.com/docs/cli/) | MIT | Checks `openapi.yaml` |
| [CNCF Distribution](https://distribution.github.io/distribution/) (optional) | Apache-2.0 | A private container registry |
| OpenSSH, rsync, systemd | BSD / GPL / LGPL | Copy builds to the servers and restart the app |

## How it fits together

```mermaid
flowchart LR
  Dev((Developer)) -->|git push| Git[Forgejo or GitHub]
  Git -->|webhook| J[Jenkins controller]
  J --> A[Build agent<br/>Go, Node, Podman]
  A -->|test branch| S[Staging server]
  A -->|main, after approval| P[Production server]
  A -.->|image, optional| R[(Registry)]
```

| Branch | Checks | Builds | Deploys |
|---|---|---|---|
| `feature/*`, `dev`, merge requests | yes | yes | no |
| `test` | yes | yes | staging, automatically |
| `main` | yes | yes | production, after someone approves |

The pipeline's stages:

1. **Checks**, in parallel: formatting and `go vet` (server and tutoring), the OpenAPI lint, and `svelte-check`.
2. **Tests**, in parallel: server and tutoring tests with the race detector against a throwaway PostgreSQL, and the frontend unit tests.
3. **Security:** `govulncheck` for both Go modules, and `npm audit` for runtime packages.
4. **Build:** the frontend, the `server` and `tutor` binaries, and a tarball with a checksum.
5. **Smoke test:** starts the new `server` on a fresh database (so every migration runs) and checks `/healthz`.
6. **Image** (optional): builds the container image with Podman and pushes it to your registry.
7. **Deploy:** copies the tarball over SSH and runs a deploy script that installs it, restarts the services and checks their health.

## 1. Install Jenkins

Run Jenkins on its own machine or VM if you can. A build agent runs whatever code is pushed, so it shouldn't share a machine with production. These commands are for Ubuntu 24.04 or Debian 12.

```bash
sudo apt update
sudo apt install -y fontconfig openjdk-21-jre
sudo wget -O /etc/apt/keyrings/jenkins-keyring.asc https://pkg.jenkins.io/debian-stable/jenkins.io-2023.key
echo "deb [signed-by=/etc/apt/keyrings/jenkins-keyring.asc] https://pkg.jenkins.io/debian-stable binary/" \
  | sudo tee /etc/apt/sources.list.d/jenkins.list
sudo apt update && sudo apt install -y jenkins
sudo cat /var/lib/jenkins/secrets/initialAdminPassword
```

If `apt update` rejects the key, check jenkins.io's install page for the current key address.

Jenkins listens on port 8080. If the quiz app runs on the same machine, it uses 8080 too, so move Jenkins to `127.0.0.1:8090` with `sudo systemctl edit jenkins`:

```ini
[Service]
Environment="JENKINS_PORT=8090"
Environment="JENKINS_LISTEN_ADDRESS=127.0.0.1"
```

Then put it behind HTTPS with Caddy (open source, Apache-2.0), which gets a certificate by itself:

```caddy
jenkins.example.edu {
	reverse_proxy 127.0.0.1:8090
}
```

Open `https://jenkins.example.edu`, paste the password, choose **Install suggested plugins** and create the admin user. In **Manage Jenkins → System**, set **Jenkins URL** to `https://jenkins.example.edu/`.

### Plugins

In **Manage Jenkins → Plugins → Available**, install these (all MIT-licensed):

- **Pipeline**, **Git** and **Pipeline: Multibranch** (included in the suggested plugins);
- **SSH Agent**, which lends an SSH key to one pipeline step;
- **Timestamper** and **Workspace Cleanup**;
- **Gitea**, if you host the code on Forgejo or Gitea (it works with both), or **GitHub Branch Source** for GitHub;
- **Email Extension**, to email the people who broke the build (optional).

### Lock it down

- **Manage Jenkins → Security:** keep "Logged-in users can do anything" switched off. Use **Matrix-based security** so only your team can run and approve builds.
- **Manage Jenkins → Nodes → Built-In Node:** set **Number of executors** to `0`, so no build runs on the controller. Builds run on the agent in the next step.

## 2. A build agent

The agent is a Linux machine (or VM) that runs the builds. Install the build tools on it:

```bash
sudo apt install -y openjdk-21-jre-headless git curl rsync build-essential podman
# Go: the version in backend/go.mod (and tutoring/go.mod)
curl -fsSL https://go.dev/dl/go1.26.3.linux-amd64.tar.gz | sudo tar -C /usr/local -xz
# Node 22
curl -fsSL https://deb.nodesource.com/setup_22.x | sudo bash -
sudo apt install -y nodejs
sudo useradd -m -s /bin/bash jenkins-agent
```

- `build-essential` provides the C compiler that Go's race detector needs.
- When `go.mod` moves to a newer Go, delete `/usr/local/go` and unpack the new release the same way.
- Podman runs rootless as `jenkins-agent`, so builds never need root. Check that it works: `sudo -iu jenkins-agent podman run --rm docker.io/library/postgres:16 postgres --version`.

Connect the agent to Jenkins:

1. In Jenkins, go to **Manage Jenkins → Nodes → New node**. Name it `linux-build`, and give it the label `linux && podman`, 2 executors, and remote root `/home/jenkins-agent`.
2. Choose **Launch agents via SSH**, with the agent's address and an SSH key for `jenkins-agent` stored as a Jenkins credential.
3. Under **Host Key Verification Strategy**, choose **Known hosts file**, then add the agent's host key to `/var/lib/jenkins/.ssh/known_hosts` on the controller.

## 3. Prepare the servers

Staging and production are set up the same way, as in [Deployment](/docs/deployment):

- the app's systemd unit is `deploy/quiz.service`, and its files are in `/srv/quiz`;
- the frontend is served from `/srv/quiz/frontend` by Caddy (`deploy/Caddyfile`);
- tutoring, if you run it, uses `deploy/quiz-tutoring.service` and `/srv/quiz-tutoring`.

On each server, add a `deploy` user that may run exactly one script as root, and nothing else:

```bash
sudo useradd -m -s /bin/bash deploy
sudo install -d -m 700 -o deploy -g deploy /home/deploy/.ssh
# paste the public half of the key Jenkins will use (step 4):
sudo tee /home/deploy/.ssh/authorized_keys > /dev/null
sudo chown deploy:deploy /home/deploy/.ssh/authorized_keys && sudo chmod 600 /home/deploy/.ssh/authorized_keys
```

Then create the deploy script, `/usr/local/bin/deploy-quiz`:

```bash
#!/usr/bin/env bash
# Installs a Jenkins build of the quiz platform and restarts it.
# Usage: deploy-quiz <tarball> <sha256>
set -euo pipefail
tarball=${1:?usage: deploy-quiz <tarball> <sha256>}
sum=${2:?usage: deploy-quiz <tarball> <sha256>}
echo "$sum  $tarball" | sha256sum -c --quiet -

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
tar -xzf "$tarball" -C "$work"
[ -x "$work/server" ] && [ -f "$work/frontend/index.html" ] || { echo "not a build: $tarball" >&2; exit 1; }

# A backup first, if the nightly backup is set up: migrations only go forward.
if systemctl cat quiz-backup.service >/dev/null 2>&1; then
	systemctl start quiz-backup.service
fi

# Keep the running version for a rollback.
cp -a /srv/quiz/server /srv/quiz/server.prev 2>/dev/null || true
rsync -a --delete /srv/quiz/frontend/ /srv/quiz/frontend.prev/ 2>/dev/null || true

install -m 755 "$work/server" /srv/quiz/server.new
mv /srv/quiz/server.new /srv/quiz/server
rsync -a --delete "$work/frontend/" /srv/quiz/frontend/
systemctl restart quiz

# The tutoring service, where it's installed.
if [ -x "$work/tutor" ] && [ -d /srv/quiz-tutoring ]; then
	cp -a /srv/quiz-tutoring/tutor /srv/quiz-tutoring/tutor.prev 2>/dev/null || true
	install -m 755 "$work/tutor" /srv/quiz-tutoring/tutor.new
	mv /srv/quiz-tutoring/tutor.new /srv/quiz-tutoring/tutor
	systemctl restart quiz-tutoring
fi

for i in $(seq 1 30); do
	if curl -fsS http://127.0.0.1:8080/healthz >/dev/null; then
		echo "deployed $(cat "$work/VERSION")"
		exit 0
	fi
	sleep 1
done
echo "the app did not become healthy; see journalctl -u quiz -n 50" >&2
exit 1
```

```bash
sudo chown root:root /usr/local/bin/deploy-quiz && sudo chmod 755 /usr/local/bin/deploy-quiz
echo 'deploy ALL=(root) NOPASSWD: /usr/local/bin/deploy-quiz *' | sudo tee /etc/sudoers.d/deploy-quiz
sudo chmod 440 /etc/sudoers.d/deploy-quiz && sudo visudo -c
```

How the script handles the risky parts:

- **Database changes:** the server runs its migrations when it starts, so the script has no separate migration step.
- **Rollback:** migrations only go forward, so before a deploy the script runs the nightly backup (`deploy/quiz-backup.service`) if it's installed. A rollback to `server.prev` works only while the new migrations are ones the old version can live with; otherwise, restore that backup.
- **Tampering:** the checksum check stops a tarball that was changed or cut short on the way.

## 4. Credentials

In **Manage Jenkins → Credentials → System → Global**, add:

| ID | Kind | What |
|---|---|---|
| `git-read` | Username with password, or SSH key | Read access to the repository (a Forgejo/GitHub access token, or a deploy key) |
| `deploy-staging` | SSH Username with private key | User `deploy`, the key whose public half is on the staging server |
| `deploy-production` | SSH Username with private key | Its own key for production; never the same as staging's |
| `registry` (optional) | Username with password | Push access to your container registry |

Make the keys with `ssh-keygen -t ed25519 -N '' -f deploy-production`. Add each server's host key to the agent's `~/.ssh/known_hosts`, so the pipeline never accepts an unknown host:

```bash
sudo -iu jenkins-agent ssh-keyscan -t ed25519 quiz.example.edu staging.quiz.example.edu >> ~/.ssh/known_hosts
```

Check the fingerprints it prints against the servers' own (`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`).

The platform's own secrets, the master key (`/etc/quiz/kek.key`) and the SMTP password, stay on the servers and never go into Jenkins. The pipeline only copies the program.

## 5. The Jenkinsfile

Add this as `Jenkinsfile` at the root of the repository. Change the names in `environment` to match your servers.

```groovy
pipeline {
  agent { label 'linux && podman' }
  options {
    disableConcurrentBuilds()
    timestamps()
    timeout(time: 45, unit: 'MINUTES')
    buildDiscarder(logRotator(numToKeepStr: '30', artifactNumToKeepStr: '10'))
  }
  environment {
    PATH = "/usr/local/go/bin:${env.HOME}/go/bin:${env.PATH}"
    GOFLAGS = '-trimpath'
    STAGING = 'deploy@staging.quiz.example.edu'
    PRODUCTION = 'deploy@quiz.example.edu'
    IMAGE = 'registry.example.edu/quiz-platform'   // leave empty to skip the image
  }
  stages {
    stage('Prepare') {
      steps {
        script {
          env.VERSION = sh(script: 'git describe --tags --always --dirty', returnStdout: true).trim()
          env.PG = "qp-ci-${env.BUILD_TAG}".replaceAll('[^A-Za-z0-9_.-]', '-')
        }
        // A throwaway PostgreSQL for this build only, on a free local port.
        sh '''
          podman run -d --rm --name "$PG" -e POSTGRES_PASSWORD=postgres \
            -p 127.0.0.1::5432 docker.io/library/postgres:16 \
            -c fsync=off -c max_connections=200
          for i in $(seq 1 30); do podman exec "$PG" pg_isready -U postgres -q && break; sleep 1; done
          podman port "$PG" 5432 | head -n1 | sed 's/.*://' > .pgport
        '''
        script { env.PGPORT = readFile('.pgport').trim() }
        dir('frontend') { sh 'npm ci --no-audit --no-fund' }
      }
    }

    stage('Checks') {
      parallel {
        stage('Go format and vet') {
          steps {
            sh '''
              for m in backend tutoring; do
                (cd $m && test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }) || exit 1
                (cd $m && go vet ./...) || exit 1
              done
            '''
          }
        }
        stage('OpenAPI') {
          steps { sh 'npx --yes @redocly/cli@latest lint backend/internal/apidocs/openapi.yaml --format=summary' }
        }
        stage('Svelte') {
          steps { dir('frontend') { sh 'npm run check' } }
        }
      }
    }

    stage('Tests') {
      parallel {
        stage('Server') {
          environment { QP_TEST_DATABASE_URL = "postgres://postgres:postgres@127.0.0.1:${env.PGPORT}/postgres?sslmode=disable" }
          steps { dir('backend') { sh 'go test -race -count=1 ./...' } }
        }
        stage('Tutoring') {
          environment { QP_TEST_DATABASE_URL = "postgres://postgres:postgres@127.0.0.1:${env.PGPORT}/postgres?sslmode=disable" }
          steps { dir('tutoring') { sh 'go test -race -count=1 ./...' } }
        }
        stage('Frontend') {
          steps { dir('frontend') { sh 'npm test' } }
        }
      }
    }

    stage('Security') {
      steps {
        sh 'go install golang.org/x/vuln/cmd/govulncheck@latest'
        dir('backend')  { sh 'govulncheck ./...' }
        dir('tutoring') { sh 'govulncheck ./...' }
        dir('frontend') { sh 'npm audit --omit=dev --audit-level=high' }
      }
    }

    stage('Build') {
      steps {
        sh '''
          rm -rf out && mkdir -p out
          (cd frontend && npm run build)
          (cd backend && CGO_ENABLED=0 go build -ldflags="-s -w" -o ../out/server ./cmd/server)
          (cd tutoring && CGO_ENABLED=0 go build -ldflags="-s -w" -o ../out/tutor ./cmd/tutor)
          cp -r frontend/build out/frontend
          echo "$VERSION" > out/VERSION
          tar -czf "quiz-$VERSION.tar.gz" -C out .
          sha256sum "quiz-$VERSION.tar.gz" | cut -d' ' -f1 > "quiz-$VERSION.tar.gz.sha256"
        '''
        archiveArtifacts artifacts: 'quiz-*.tar.gz*', fingerprint: true
      }
    }

    stage('Smoke test') {
      // Every migration from an empty database, then the health check.
      steps {
        sh '''
          podman exec "$PG" createdb -U postgres smoke
          head -c 32 /dev/urandom > smoke.kek && chmod 400 smoke.kek
          QP_DATABASE_URL="postgres://postgres:postgres@127.0.0.1:$PGPORT/smoke?sslmode=disable" \
          QP_KEK_FILE="$PWD/smoke.kek" QP_LISTEN=127.0.0.1:18080 QP_BASE_URL=http://127.0.0.1:18080 \
            ./out/server > smoke.log 2>&1 &
          pid=$!
          ok=1
          for i in $(seq 1 30); do curl -fsS http://127.0.0.1:18080/healthz && { ok=0; break; }; sleep 1; done
          kill $pid || true
          [ $ok -eq 0 ] || { cat smoke.log; exit 1; }
        '''
      }
    }

    stage('Image') {
      when { allOf { anyOf { branch 'test'; branch 'main' }; expression { env.IMAGE } } }
      steps {
        withCredentials([usernamePassword(credentialsId: 'registry', usernameVariable: 'RU', passwordVariable: 'RP')]) {
          sh '''
            podman build -t "$IMAGE:$VERSION" .
            echo "$RP" | podman login --username "$RU" --password-stdin "${IMAGE%%/*}"
            podman push "$IMAGE:$VERSION"
            podman logout "${IMAGE%%/*}"
          '''
        }
      }
    }

    stage('Deploy to staging') {
      when { branch 'test' }
      steps { deployTo('deploy-staging', env.STAGING) }
    }

    stage('Deploy to production') {
      when { branch 'main' }
      steps {
        timeout(time: 2, unit: 'HOURS') {
          input message: "Deploy ${env.VERSION} to production?", ok: 'Deploy', submitter: 'release-managers'
        }
        deployTo('deploy-production', env.PRODUCTION)
      }
    }
  }
  post {
    always {
      sh 'podman rm -f "$PG" >/dev/null 2>&1 || true; rm -f smoke.kek'
      cleanWs(deleteDirs: true, notFailBuild: true)
    }
  }
}

// Copies the tarball to a server and runs its deploy script.
def deployTo(String credentialsId, String target) {
  sshagent(credentials: [credentialsId]) {
    sh """
      f=quiz-\$VERSION.tar.gz
      scp -o BatchMode=yes "\$f" ${target}:/tmp/\$f
      ssh -o BatchMode=yes ${target} "sudo /usr/local/bin/deploy-quiz /tmp/\$f \$(cat \$f.sha256) && rm -f /tmp/\$f"
    """
  }
}
```

Some notes on the Jenkinsfile:

- **The test database:** each build starts its own PostgreSQL container on a random local port and removes it afterwards, so builds running at once never share data. The server and tutoring tests create their own databases, named after the test process (`dbtest`, `testenv`), so they can share one server.
- **Who can deploy:** `submitter: 'release-managers'` is a Jenkins group or user list. Only they see the **Deploy** button.
- **Waiting for approval:** a build waiting for approval keeps its agent busy. If that's a problem, put the input step in its own stage with `agent none`.
- **Skipping the image:** without a registry, set `IMAGE = ''`. The image stage is skipped and the servers still get the binaries.
- **Pinning versions:** `@latest` for Redocly and govulncheck keeps them current. For builds you can repeat exactly, pin them to a version.

## 6. Create the job

1. **New Item → Multibranch Pipeline**, named `quiz-platform`.
2. **Branch Sources:**
   - **Forgejo or Gitea:** add the server under **Manage Jenkins → System → Gitea Servers** first, then choose *Gitea*, the `git-read` credential, the owner and the repository.
   - **GitHub:** choose *GitHub*, with the `git-read` credential and the repository URL.
   - Under **Behaviours**, keep *Discover branches*, and add *Discover pull requests from origin* so merge requests are checked before they're merged.
   - Add *Discover tags* and *Advanced clone behaviours* → *Fetch tags*: `git describe` needs the tags to name the build.
3. **Build Configuration:** *by Jenkinsfile*, path `Jenkinsfile`.
4. **Scan Repository Triggers:** tick *Periodically if not otherwise run*, every 1 day. That's only a fallback for when a webhook is missed.
5. Save. Jenkins finds the branches and builds each one.

Then add a webhook so builds start at once:

- **Forgejo or Gitea:** repository **Settings → Webhooks → Add webhook → Gitea**, URL `https://jenkins.example.edu/gitea-webhook/post`, content type `application/json`, with *Push* and *Pull request* events.
- **GitHub:** repository **Settings → Webhooks**, URL `https://jenkins.example.edu/github-webhook/`, content type `application/json`.

Webhooks reach Jenkins over HTTPS. Allow only your Git server's address to `/gitea-webhook/` or `/github-webhook/` if Jenkins is on the internet. In Caddy, use a `remote_ip` matcher.

## 7. Releasing

The release flow with the pipeline:

1. Merge a feature into `dev` with `--no-ff`. Jenkins checks it.
2. Promote `dev` to `test`. Jenkins checks it and deploys to staging, where the team tries it.
3. Merge `test` into `main` and tag it (`git tag -a v2.9.0`). Jenkins builds it, then waits.
4. A release manager opens the build, reads its version, and clicks **Deploy**.

`VERSION` comes from `git describe`, so a tagged build is named after its tag (`v2.9.0`) and anything else after the last tag plus the commit (`v2.9.0-3-gabc1234`). It's written to the tarball's `VERSION` file and printed by the deploy script.

### Rolling back

**The quick way**, when the new version hasn't changed the database in a way the old one can't handle:

```bash
ssh deploy@quiz.example.edu
sudo mv /srv/quiz/server.prev /srv/quiz/server
sudo rsync -a --delete /srv/quiz/frontend.prev/ /srv/quiz/frontend/
sudo systemctl restart quiz
```

`sudo` here needs your admin account, not `deploy`.

**From Jenkins:** open an older `main` build and choose **Rebuild** or **Replay**. Its tarball is kept with the build (the last 10).

**If a migration is the problem:** restore the backup taken just before the deploy (see [Deployment](/docs/deployment#restoring-a-backup)), then roll back.

## 8. Upkeep

- **Jenkins and plugins:** update monthly from **Manage Jenkins → Plugins → Updates**, and run the LTS release (`apt upgrade jenkins`). Jenkins publishes security advisories for plugins; subscribe to them.
- **Go and Node:** when `go.mod` or the Node version changes, update the agent too. A build on the old Go fails at once with a clear "go.mod requires go >= …" message.
- **Podman storage:** old images pile up on the agent. Clear them weekly with a cron job: `podman system prune -af --filter until=168h`.
- **Backing up Jenkins:** back up `/var/lib/jenkins`. The jobs, credentials and build history are all there; the credentials are encrypted with keys kept in the same folder (`secrets/`).

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Tests fail with `connection refused` | PostgreSQL wasn't ready yet, or rootless Podman can't publish ports. Run `podman info` as `jenkins-agent`, and check that `/etc/subuid` and `/etc/subgid` have a line for that user |
| `-race requires cgo` | Install `build-essential` on the agent |
| `go: go.mod requires go >= 1.x` | The agent's Go is older than `go.mod`: install the newer release |
| `npm audit` fails the build | A runtime package has a known high or critical issue. Update it in a feature branch; don't lower the level |
| The deploy hangs at `ssh` | The host key isn't in the agent's `known_hosts`. `BatchMode=yes` turns this into an error instead of a prompt |
| `sudo: a password is required` | The sudoers line doesn't match the command exactly. It must be `/usr/local/bin/deploy-quiz *` |
| The deploy fails with "did not become healthy" | Look at `journalctl -u quiz -n 50` on the server. Usually it's a setting or the database; roll back as above |
| The smoke test fails but the tests pass | A migration fails on an empty database, or the server needs a new setting. `smoke.log` is printed in the build log |
