# aws-compass

Opens MongoDB Compass for a cluster that uses AWS IAM authentication, with
temporary credentials from an AWS SSO profile. It runs `aws sso login` only
when the cached session can no longer produce credentials.

```
aws-compass my-cluster
```

It works around Compass failing with "nt is not a function" when the AWS IAM
fields are left empty: aws-compass gets the credentials with
`aws configure export-credentials` and passes Compass a full connection string.

## Install

Requires the AWS CLI v2. Building requires Go 1.24 or newer.

```bash
go build -ldflags "-X main.version=1.0.0" -o ~/bin/aws-compass .
```

Or use a prebuilt binary from `dist/` (`darwin-arm64` for Apple silicon,
`darwin-amd64` for Intel). A downloaded binary is quarantined by macOS, so
clear that once:

```bash
xattr -d com.apple.quarantine ~/bin/aws-compass
```

## Config

Add connections from the command line:

```bash
aws-compass --save my-cluster \
  --uri mongodb+srv://my-cluster-pl-0.xmbso.mongodb.net --profile my-aws-profile
aws-compass --save my-cluster --profile other-profile   # change one field
```

This creates the config file if needed. A new connection needs `--uri`;
for an existing one only the fields you pass change. The file is rewritten
as formatted JSON, and a symlinked config (e.g. in a dotfiles repo) is
updated in place.

Or edit the file directly: `~/.config/aws-compass/config.json` (or `$XDG_CONFIG_HOME/aws-compass/config.json`).
The legacy `~/.aws-compass/config` is also read. Override with `--config` or
`$AWS_COMPASS_CONFIG`.

```json
{
  "defaults": {
    "profile": "my-aws-profile",
    "compassPath": "/Applications/MongoDB Compass.app/Contents/MacOS/MongoDB Compass",
    "mongoshPath": "mongosh"
  },
  "connections": {
    "my-cluster": {
      "uri": "mongodb+srv://my-cluster-pl-0.xmbso.mongodb.net",
      "profile": "my-aws-profile"
    }
  }
}
```

- `connections` maps a name to a cluster. `uri` has no credentials and may
  include a database and query parameters (`appName`, `readPreference`, ...);
  aws-compass adds `authMechanism`, `authSource` and the session token.
- `profile` is a profile from `~/.aws/config`. Priority: `--profile`, the
  connection's `profile`, `defaults.profile`, then `$AWS_PROFILE`.
- `defaults.compassPath` and `defaults.mongoshPath` are optional, e.g. for
  Compass Beta or Isolated Edition.
- Unknown keys are rejected, so typos show up as errors.

## Usage

```
aws-compass <connection>                 open Compass
aws-compass <connection> --print         print the connection string (secrets masked)
aws-compass <connection> --print --reveal
aws-compass <connection> --copy          copy the full connection string to the clipboard
aws-compass <connection> --mongosh       open mongosh instead
aws-compass <connection> --login         force aws sso login
aws-compass --uri mongodb+srv://host --profile my-aws-profile
aws-compass --list
aws-compass --save <name> [--uri <uri>] [--profile <profile>]
```

Flags can go before or after the connection name. Status messages go to
stderr, so `--print` output can be piped.

## Notes

- Compass doesn't refresh the credentials. When they expire, quit Compass and
  run aws-compass again. The expiry time is printed when you connect.
- The full connection string is visible in the process list while Compass runs.
- `--mongosh` passes the profile (`AWS_PROFILE`) instead of credentials:
  mongosh reads the SSO session itself and refreshes it, and no secrets appear
  in the process list.
- If Compass is already open, quit it first if the new connection doesn't
  appear, or use `--copy` and paste into the open window.

## Dependencies

- [kong](https://github.com/alecthomas/kong): command-line parsing and help,
  declared as the `CLI` struct in `main.go`.
- [atotto/clipboard](https://github.com/atotto/clipboard): `--copy`.

Everything else, including the AWS calls (via the `aws` CLI) and connection
string handling, uses the standard library.

## Development

```bash
go test ./...
go vet ./...
```

See `CLAUDE.md` for the code layout and conventions.
