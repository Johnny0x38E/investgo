# Repository Guidelines

## Project Structure

InvestGo is a Go/Wails desktop application. `main.go` boots the app and embeds
the built frontend. Backend code lives in `internal/`: `api` contains HTTP
routes, `core` contains domain, store, provider, market-data, and FX logic,
`platform` handles OS/window/proxy integration, and `logger` owns diagnostics.
The Vue/TypeScript UI is under `frontend/src`, especially `components`,
`composables`, `styles`, `api.ts`, and `types.ts`. Platform build/package
scripts and the icon pipeline are in `scripts/`; source artwork is in
`assets/` and `frontend/src/assets/`. Treat `build/` and `frontend/dist/` as
generated output. Add Go tests beside the package under test using `*_test.go`.

## Build, Test, and Development Commands

Requirements are Node.js 20+, pnpm 11+, and Go 1.24+.

- `pnpm install` installs frontend dependencies.
- `pnpm dev` starts the Vite frontend development server.
- `pnpm typecheck` runs `vue-tsc` in strict mode; `pnpm build` builds the frontend.
- `env GOCACHE=/tmp/go-build-cache go test ./...` compiles and runs all Go tests.
- `./scripts/build-darwin-aarch64.sh` or `./scripts/build-darwin-x86_64.sh` builds macOS binaries.
- `VERSION=1.0.0 ./scripts/package-darwin-aarch64.sh` creates a macOS DMG; use the Windows PowerShell script for Windows builds.

## Coding Style and Naming

Run `gofmt` on Go changes and use idiomatic Go naming. For Vue/TypeScript,
follow `prettier.config.js`: four-space indentation, 120-column width,
single quotes, trailing commas, and LF endings. Keep TypeScript strict and
keep frontend API types synchronized with backend responses. Use lowercase
Go package names, PascalCase exported Go symbols, and descriptive
camelCase functions and variables in TypeScript.

## Testing Guidelines

The repository currently has no dedicated frontend test runner or checked-in
test suite. Run `pnpm typecheck` and the Go test command for every change;
add focused `*_test.go` coverage for new backend behavior and document any
platform-only validation.

## Commits and Pull Requests

Recent commits use short, lowercase prefixes such as `fix:`, `chore:`, and
`refactor:`. Keep commits focused and describe the user-visible or technical
change. Pull requests should include a concise summary, validation commands
and results, linked issue context when available, and screenshots or a short
recording for UI changes. For packaging work, state the target OS, version,
and produced artifact path.

## Security and Configuration

Do not commit provider API keys, proxy credentials, local state, logs, or
generated binaries. Avoid placing secrets in logs or screenshots; use the
application's local settings and redaction behavior when testing integrations.
