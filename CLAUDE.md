# CLAUDE.md

Guidance for working on this repository.

## What this is

`aws-compass` is a small Go CLI that opens MongoDB Compass (or mongosh) for a
MongoDB cluster using AWS IAM authentication (`MONGODB-AWS`). It gets
temporary credentials from an AWS SSO profile and passes Compass a full
connection string.

Why it exists: Compass crashes with "nt is not a function" when its AWS IAM
fields are left empty, so it can't use the AWS credential chain itself.
mongosh doesn't have that bug, which is why `--mongosh` passes `AWS_PROFILE`
instead of credentials.

## Commands

```bash
go build -o aws-compass .
go test ./...
go vet ./...
gofmt -l .        # must print nothing

# macOS release binaries
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=X.Y.Z" -o dist/aws-compass-darwin-arm64 .
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=X.Y.Z" -o dist/aws-compass-darwin-amd64 .
```

## Layout

| File | Contents |
|---|---|
| `main.go` | `CLI` struct (kong flags, `Validate`), `Run` dispatch, open/save/list |
| `config.go` | Config types, config path lookup, load (strict JSON) and atomic save |
| `uri.go` | Connection string parsing and adding AWS auth (`buildURI`) |
| `aws.go` | `Credentials`, calls to the `aws` CLI, expiry text |
| `launch.go` | Starting Compass (detached) and mongosh, clipboard |
| `detach_*.go` | Per-OS process detaching (build tags) |
| `*_test.go` | Unit tests |

Flow for `aws-compass <name>`: parse flags → `Run` → `open` → `resolve`
(connection + overrides + defaults) → `getCredentials` → `buildURI` → launch.

## Conventions

- **Dependencies:** only kong and atotto/clipboard. Keep everything else on
  the standard library. Don't add the AWS SDK: it can't do the browser-based
  `aws sso login`, and it doesn't share the CLI's role-credential cache, so
  the `aws` CLI stays the single source of credentials.
- **Output:** stdout only for results (`--print`, `--list`), so they can be
  piped. Status messages and errors go to stderr. Exit code 2 for usage
  errors, 1 for other failures; mongosh's own exit code is passed through.
- **Secrets:** never print the secret access key or session token unmasked,
  except with `--print --reveal`. Never write credentials to the config file.
  Don't log the full connection string.
- **Flags:** add a field to the `CLI` struct with a `help` tag. Put rules
  that involve several flags in `CLI.Validate` (or kong's `xor` tags).
- **Config:** the file is strict JSON (`DisallowUnknownFields`), so a new
  config field must be added to the structs in `config.go` and documented in
  `README.md` and `config.example.json`. Saving goes through `saveConfig`,
  which writes atomically and follows symlinks.
- **Connection strings:** don't switch to `net/url` for parsing, since it
  rejects seed lists (`h1:27017,h2:27017`). Secrets and tokens are encoded
  with `encode` (everything except unreserved characters). Parameters the
  tool doesn't touch must come out byte-for-byte unchanged.
- **Errors:** wrap with context (`fmt.Errorf("doing x: %w", err)`), lower-case,
  and say how to fix it when possible.

## Testing

- `uri_test.go` covers `buildURI` with exact expected strings. When changing
  connection string behaviour, update these and keep the round-trip test.
- `main_test.go` covers flag parsing (via `kong.New`) and `--save`.
- There's no real AWS or Compass in tests. For an end-to-end check, put fake
  `aws`, `compass` and `mongosh` scripts on `PATH`, point `defaults.compassPath`
  at the fake Compass, and set `XDG_CONFIG_HOME` to a temp directory. The fake
  `aws` should answer `configure export-credentials` with JSON like
  `{"Version":1,"AccessKeyId":"ASIA...","SecretAccessKey":"...","SessionToken":"...","Expiration":"2099-01-01T00:00:00Z"}`
  and fail until `sso login` has been called, to exercise the login retry.
