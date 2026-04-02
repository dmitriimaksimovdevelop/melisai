# Website: melisai.dev

## Overview

melisai.dev is the project website hosted on Kubernetes (Hetzner Cloud). It consists of a landing page, install script endpoint, and full documentation site built from the `doc/` directory of this repo.

## Architecture

```
melisai repo (this repo)          hetzner-k8s-infra repo
├── doc/en/*.md  (22 chapters)    ├── apps/melisai-site/
├── doc/ru/*.md  (22 chapters)    │   ├── melisai/        ← git submodule (this repo)
└── context/WEBSITE.md (this)     │   │   └── doc/en/, doc/ru/
                                  │   ├── public/
                                  │   │   ├── index.html  ← landing page
                                  │   │   └── install.sh  ← one-liner installer
                                  │   ├── mkdocs.yml      ← mkdocs-material config
                                  │   ├── build-docs.sh   ← prepares docs from submodule
                                  │   ├── nginx.conf
                                  │   ├── Dockerfile       ← multi-stage: mkdocs build + nginx
                                  │   ├── werf.yaml
                                  │   └── .helm/           ← Helm chart (app-template pattern)
                                  └── terraform/
                                      ├── dns.tf          ← hcloud_zone melisai.dev
                                      └── cert_manager.tf ← TLS cert for melisai.dev
```

## Repos

| Repo | URL | Purpose |
|------|-----|---------|
| **melisai** | `github.com/dmitriimaksimovdevelop/melisai` | Go binary, source code, docs source (doc/en, doc/ru) |
| **hetzner-k8s-infra** | `github.com/dmitriimaksimovdevelop/hetzner-k8s-infra` | K8s infra, website app, DNS, TLS, CI/CD |

## URLs

| URL | What |
|-----|------|
| `https://melisai.dev` | Landing page |
| `https://melisai.dev/install` | Install script (`curl -sSL https://melisai.dev/install \| sh`) |
| `https://melisai.dev/docs/` | Documentation (English, mkdocs-material) |
| `https://melisai.dev/docs/ru/` | Documentation (Russian) |

## How to Update Documentation on the Website

Documentation lives in **two places** and must be synced:

### Source of truth: `melisai/doc/`

All documentation is authored in this repo under `doc/en/` and `doc/ru/`. This is the canonical source.

### Website copy: `hetzner-k8s-infra/apps/melisai-site/docs/`

The infra repo contains a **copy** of the docs that mkdocs builds into HTML.

### Update procedure (git submodule)

melisai repo is a **git submodule** inside hetzner-k8s-infra at `apps/melisai-site/melisai/`.
The Dockerfile copies only `melisai/doc/` into the build context — no Go code enters the image.

1. Edit docs in `melisai/doc/en/` and/or `melisai/doc/ru/`
2. Commit and push to melisai repo
3. Update submodule in infra repo:
   ```bash
   cd hetzner-k8s-infra
   cd apps/melisai-site/melisai
   git pull origin master
   cd ../../..
   git add apps/melisai-site/melisai
   git commit -m "chore: update melisai submodule (docs update)"
   git push
   ```
4. If new chapters were added, also update `nav:` section in `mkdocs.yml`
5. Deploy triggers automatically via werf (push to `apps/**`)

### How Docker build works

`build-docs.sh` runs inside the Docker build:
1. Copies EN docs from `submodule-doc/en/*.md` → `docs/`
2. Renames `00-introduction.md` → `index.md`
3. Copies RU docs with `.ru.md` suffix for i18n plugin
4. Runs `mkdocs build` → static HTML in `/build/site`

Only `melisai/doc/` is COPYed into Docker — the Go source code never enters the image.

## Infrastructure Details

### DNS (terraform/dns.tf)

```hcl
resource "hcloud_zone" "melisai" {
  name = "melisai.dev"
}
# A records: @, www → Traefik LB IP
```

**Prerequisites:** melisai.dev nameservers must point to Hetzner DNS:
- `hydrogen.ns.hetzner.com`
- `oxygen.ns.hetzner.com`
- `helium.ns.hetzner.de`

### TLS (terraform/cert_manager.tf)

Let's Encrypt via cert-manager, HTTP-01 challenge through Traefik Gateway API.

Certificate covers: `melisai.dev`, `www.melisai.dev`

### Deployment

werf-based CI/CD:
- Push to `apps/melisai-site/**` triggers deploy
- Or: `workflow_dispatch` with `app=melisai-site`
- Docker image: multi-stage (python mkdocs → nginx alpine)
- Helm chart: app-template pattern (Deployment + Service)
- HTTPRoute needed for Traefik Gateway API (not yet created — add to .helm/templates/)

### Stack

| Component | Version | Role |
|-----------|---------|------|
| mkdocs-material | latest | Static site generator for docs |
| mkdocs-static-i18n | latest | EN/RU language support |
| nginx | 1.27-alpine | Web server |
| werf | v2 | CI/CD, image build, deploy |
| cert-manager | 1.15.3 | TLS certificates |
| Traefik | v3.1 | Ingress / Gateway API |

## Landing Page

`public/index.html` — standalone HTML with inline CSS (no build tools needed). Dark theme matching GitHub aesthetic. Features:
- Hero with install one-liner (click to copy)
- Stats bar (67 tools / 37 rules / 8 collectors / 10s)
- Terminal demo with colored output
- 9-block feature grid
- Nav links to docs (EN/RU) and GitHub

## Install Script

`public/install.sh` — served at `/install` as `text/plain`. Features:
- Detects OS (linux only) and arch (amd64/arm64)
- Fetches latest release tag from GitHub API
- Downloads tarball from GitHub Releases
- Installs to /usr/local/bin (sudo if needed)
- Cleanup via trap

Depends on goreleaser producing `melisai_<version>_linux_<arch>.tar.gz` artifacts.

## Checklist for New Releases

When releasing a new version of melisai:

1. [ ] Tag and push: `git tag v0.X.Y && git push --tags`
2. [ ] goreleaser creates GitHub Release with binaries
3. [ ] Install script automatically picks up latest release
4. [ ] Update landing page stats if anomaly/tool counts changed
5. [ ] Sync docs to infra repo if documentation changed
6. [ ] Deploy site: `workflow_dispatch` app=melisai-site
