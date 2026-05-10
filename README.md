# gitea2github

A lightweight service that mirrors Gitea repositories to GitHub automatically via webhooks.

## How it works

1. Configure Gitea to send push webhooks to this service.
2. On each push event the service writes an async task file to a queue directory.
3. A background goroutine polls the queue every **5 seconds**, exclusively locks the task file, performs a `git fetch --mirror` from Gitea followed by a `git push --mirror` to GitHub, then deletes the file.
4. Each operation is recorded in a rolling log file (maximum **100 entries**; older entries are pruned automatically).

## Features

- **Single Gitea account, multiple GitHub accounts** – one global Gitea HTTP/HTTPS user is used for every project; any number of GitHub SSH credentials can be configured.
- **Per-project mapping** – each project independently specifies which Gitea repo and GitHub repo to mirror, and which GitHub credential to use.
- **Webhook signature validation** – HMAC-SHA256 (`X-Gitea-Signature`) verified globally or per-project.
- **Async, deduplicated queue** – rapid consecutive pushes to the same repo are collapsed into a single sync operation.
- **File locking** – ensures no two goroutines sync the same project concurrently.
- **Docker / GHCR** – a multi-stage `Dockerfile` is included; a GitHub Actions workflow automatically builds and pushes the image to GHCR on every push to `main` and on version tags.

## Quick start

### 1. Copy and edit the config

```bash
cp config.yaml.example config.yaml
$EDITOR config.yaml
```

### 2. Run directly

```bash
go build -o gitea2github ./cmd
./gitea2github -config config.yaml
```

### 3. Run with Docker

```bash
docker run -d \
  -p 8080:8080 \
  -v $(pwd)/config.yaml:/home/app/config.yaml:ro \
  -v /path/to/ssh/keys:/home/app/.ssh:ro \
  -v gitea2github-data:/data \
  ghcr.io/nathan77886/gitea2github:main
```

## Configuration reference

| Key | Default | Description |
|-----|---------|-------------|
| `server.port` | `8080` | HTTP listen port |
| `server.secret` | `""` | Global webhook HMAC-SHA256 secret |
| `work_dir` | `work` | Directory for local bare-clone mirrors |
| `queue_dir` | `queue` | Directory for async queue files |
| `log_file` | `sync.log` | Rolling sync log path |
| `gitea.username` | – | Username for the global Gitea HTTP(S) account |
| `gitea.password` | – | Password or personal access token for the global Gitea account |

> **Note:** Gitea is supported over HTTP/HTTPS only. The `gitea_repo` URL on
> each project must use `http://` or `https://`. A single global `gitea`
> account is shared by every project; per-project `gitea_credential` is no
> longer accepted and a config that still contains it will fail to load.

See [`config.yaml.example`](config.yaml.example) for a fully annotated example.

## Gitea webhook setup

In your Gitea project → **Settings → Webhooks → Add Webhook → Gitea**:

- **Target URL**: `http://<host>:8080/webhook`
- **Secret**: the value you put in `server.secret` or the project's `secret` field
- **Trigger**: *Push Events*

## GitHub Actions / GHCR

The workflow in `.github/workflows/docker.yml` builds and pushes the Docker image to GHCR automatically:

- **On push to `main`** → `ghcr.io/<owner>/gitea2github:main`
- **On version tags** (`v*`) → `ghcr.io/<owner>/gitea2github:<version>`
- **On pull requests** → image is built but not pushed

## License

MIT
