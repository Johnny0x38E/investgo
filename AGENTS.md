# Repository Guidelines

## Project Structure

InvestGo is a Go/Wails desktop application. `main.go` boots the app and embeds
the built frontend plus `build/appicon.png`. Backend code lives in `internal/`:
`api` contains HTTP routes, `core` contains domain, store, pool, provider,
market-data, and FX logic, `storage/sqlite` is the live persistence layer,
`platform` handles OS/window/proxy integration, and `logger` owns diagnostics.
The Vue/TypeScript UI is under `frontend/src`, especially `components`,
`composables`, `styles`, `api.ts`, and `types.ts`. Platform build/package
scripts and the icon pipeline are in `scripts/`; source artwork is in
`assets/` and `frontend/src/assets/`. GitHub Actions live in
`.github/workflows/`. Treat `build/` and `frontend/dist/` as generated output.
Add Go tests beside the package under test using `*_test.go`.

## Build, Test, and Development Commands

Requirements are Node.js 22.13+, pnpm 11+, and Go 1.27+.

- `pnpm install` installs frontend dependencies.
- `pnpm dev` starts the Vite frontend development server.
- `pnpm typecheck` runs `vue-tsc` in strict mode; `pnpm build` builds the frontend.
- `go test ./...` compiles every package, including `main.go`. Prepare the
  embeds first, matching CI:

```bash
mkdir -p build
cp scripts/icon/appicon.png build/appicon.png
pnpm build
env GOCACHE=/tmp/go-build-cache go test ./...
```

`./scripts/icon/process/render-app-icon.sh` is the equivalent way to produce
`build/appicon.png`. Local macOS package scripts also run it themselves.

GitHub Actions is the default verification and release path:

- Pushes to `main` and pull requests run `.github/workflows/ci.yml` (Node 22,
  pnpm, frontend typecheck/build, then `go test ./...`).
- Pushing a `v*` tag runs `.github/workflows/release.yml`: Apple Silicon DMG,
  Intel DMG, Windows `.exe`, then a GitHub Release. Manual **Release** runs
  from the Actions tab skip the GitHub Release and only upload artifacts.

Prefer a new patch tag (`v0.3.2`, `v0.3.3`, …) over moving an existing tag.
The release workflow is taken from the tagged commit, so a moved tag only
helps if that commit already contains the workflow and packaging fixes.

Local packaging is optional and writes to `build/bin/`:

- `VERSION=1.0.0 ./scripts/package-darwin-aarch64.sh` or
  `./scripts/package-darwin-x86_64.sh`
- `.\scripts\build-windows-amd64.ps1` on Windows

macOS desktop builds pass `-tags private_mac_apis`. Wails v3 beta.19 and later
compile Liquid Glass webview transparency and `OpenDevTools` as no-ops unless
that tag is set. `scripts/build-darwin-aarch64.sh` adds it by default; the
Intel build script and both package scripts call that path, and so does
`.github/workflows/release.yml`. Set `PRIVATE_MAC_APIS=0` for an App Store or
public-API-only binary. `--dev` still adds the `devtools` tag and the
DevTools ldflags; on macOS the inspector call also needs `private_mac_apis`.
The tag is macOS-only, so the Windows script does not pass it. `go test`
does not need it: the window tests only check option values.

## Coding Style and Naming

Run `gofmt` on Go changes and use idiomatic Go naming. For Vue/TypeScript,
follow `prettier.config.js`: four-space indentation, 120-column width,
single quotes, trailing commas, and LF endings. Keep TypeScript strict and
keep frontend API types synchronized with backend responses. Use lowercase
Go package names, PascalCase exported Go symbols, and descriptive
camelCase functions and variables in TypeScript.

## Testing Guidelines

The repository currently has no dedicated frontend test runner or checked-in
frontend test suite. For every change, run `pnpm typecheck` and the Go test
command above (including the embed prep). Add focused `*_test.go` coverage
for new backend behavior. Treat a green `CI` workflow on GitHub as the
authoritative check; document any platform-only validation that CI cannot
cover.

## Commits and Pull Requests

Recent commits use short, lowercase prefixes such as `fix:`, `chore:`, and
`refactor:`. Keep commits focused and describe the user-visible or technical
change. Pull requests should include a concise summary, validation commands
and results, linked issue context when available, and screenshots or a short
recording for UI changes. For packaging work, state the target OS, version,
and produced artifact path. Release tags use a `v` prefix (`v0.3.2`); the
workflow does not match unprefixed tags.

## Security and Configuration

Do not commit provider API keys, proxy credentials, local state, logs,
SQLite databases, or generated binaries. Live user data is
`investgo.db` under the OS config directory (on macOS,
`~/Library/Application Support/investgo/`); leftover `state.json` is
legacy. Avoid placing secrets in logs or screenshots; use the application's
local settings and redaction behavior when testing integrations.

`PUT /api/settings` is a partial update. Omitted JSON fields keep their stored
values, including API keys and `developerMode` / `useNativeTitleBar`. The Vue
settings form still sends the full `AppSettings` object. A present empty
`proxyURL` or API key clears that secret on purpose. `POST /api/client-logs`
redacts the same provider-key patterns as the store and rejects oversized
bodies or batches.
