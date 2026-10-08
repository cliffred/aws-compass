// aws-compass opens MongoDB Compass (or mongosh) for a MongoDB cluster that
// uses AWS IAM authentication, with temporary credentials from an AWS SSO
// profile. See README.md.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/alecthomas/kong"
)

var version = "dev" // set with -ldflags "-X main.version=..."

const description = `Opens MongoDB Compass with temporary AWS credentials from an AWS SSO profile,
running 'aws sso login' first when the session has expired.

Examples:
  aws-compass my-cluster
  aws-compass my-cluster --print
  aws-compass --uri mongodb+srv://host --profile my-aws-profile
  aws-compass --save new-cluster --uri mongodb+srv://host --profile my-aws-profile`

// CLI defines the command line. Kong builds the parser and --help from it.
type CLI struct {
	Connection string `arg:"" optional:"" help:"Name of a connection in the config file."`

	Profile string `short:"p" placeholder:"NAME" help:"AWS profile to use (overrides the config)."`
	URI     string `short:"u" name:"uri" placeholder:"URI" help:"Connection string without credentials (overrides the config)."`

	List    bool   `short:"l" help:"List configured connections."`
	Print   bool   `xor:"mongosh-print" help:"Print the connection string instead of opening Compass (secrets masked)."`
	Reveal  bool   `help:"With --print: show the secret and session token."`
	Copy    bool   `xor:"mongosh-copy" help:"Copy the connection string to the clipboard instead of opening Compass."`
	Mongosh bool   `xor:"mongosh-print,mongosh-copy" help:"Open mongosh instead of Compass."`
	Login   bool   `help:"Run 'aws sso login' even if the session is still valid."`
	Save    string `placeholder:"NAME" help:"Save --uri and/or --profile as connection NAME in the config (creates the file if needed)."`

	Config  string           `type:"path" placeholder:"PATH" help:"Config file (default ~/.config/aws-compass/config.json)."`
	Version kong.VersionFlag `help:"Print the version."`
}

// Validate is called by kong after parsing.
func (c *CLI) Validate() error {
	if c.Reveal && !c.Print {
		return errors.New("--reveal only works together with --print")
	}
	if c.Save != "" {
		if strings.TrimSpace(c.Save) == "" {
			return errors.New("--save needs a connection name")
		}
		if c.URI == "" && c.Profile == "" {
			return errors.New("--save needs --uri and/or --profile")
		}
		if c.Connection != "" || c.List || c.Print || c.Copy || c.Mongosh || c.Login {
			return errors.New("--save can only be combined with --uri, --profile and --config")
		}
	}
	return nil
}

// errNoAction means nothing was asked for; main shows the usage.
var errNoAction = errors.New("no connection given")

// exitCode is an error that only carries a process exit status.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func main() {
	var cli CLI
	parser := kong.Must(&cli,
		kong.Name("aws-compass"),
		kong.Description(description),
		kong.Vars{"version": "aws-compass " + version},
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	)
	if _, err := parser.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "aws-compass: %v\nRun 'aws-compass --help' for usage.\n", err)
		os.Exit(2)
	}

	err := cli.Run(os.Stdout, os.Stderr)
	var code exitCode
	switch {
	case err == nil:
	case errors.Is(err, errNoAction):
		fmt.Fprintln(os.Stderr, "Usage: aws-compass <connection> [flags]\nRun 'aws-compass --help' for more, or 'aws-compass --list' for your connections.")
		os.Exit(2)
	case errors.As(err, &code):
		os.Exit(int(code))
	default:
		fmt.Fprintf(os.Stderr, "aws-compass: %v\n", err)
		os.Exit(1)
	}
}

// Run does what the parsed command line asks. Results go to stdout, status
// messages to stderr.
func (c *CLI) Run(stdout, stderr io.Writer) error {
	path, explicit, err := configPath(c.Config)
	if err != nil {
		return err
	}

	switch {
	case c.List:
		return c.list(stdout, path, explicit)
	case c.Save != "":
		return c.save(stderr, path)
	case c.Connection == "" && c.URI == "":
		return errNoAction
	default:
		return c.open(stdout, stderr, path, explicit)
	}
}

// open gets credentials and opens Compass or mongosh, or prints/copies the
// connection string.
func (c *CLI) open(stdout, stderr io.Writer, path string, explicit bool) error {
	cfg, found, err := loadConfig(path, explicit)
	if err != nil {
		return err
	}
	conn, err := c.resolve(cfg, path, found)
	if err != nil {
		return err
	}

	creds, err := getCredentials(conn.Profile, c.Login, stderr)
	if err != nil {
		return err
	}
	if expiry := describeExpiry(creds.Expiration, time.Now()); expiry != "" {
		fmt.Fprintf(stderr, "Credentials for %s %s\n", conn.Profile, expiry)
	}

	if c.Mongosh {
		uri, err := buildURI(conn.URI, nil, false)
		if err != nil {
			return err
		}
		return runMongosh(cfg.Defaults.MongoshPath, uri, conn.Profile)
	}

	fullURI, err := buildURI(conn.URI, creds, false)
	if err != nil {
		return err
	}

	if c.Print || c.Copy {
		return c.printOrCopy(stdout, stderr, conn.URI, creds, fullURI)
	}

	compass, err := compassPath(cfg)
	if err != nil {
		return err
	}
	if err := launchCompass(compass, fullURI); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "Opening MongoDB Compass for %s\n", conn.label())
	return nil
}

func (c *CLI) printOrCopy(stdout, stderr io.Writer, baseURI string, creds *Credentials, fullURI string) error {
	if c.Print {
		shown := fullURI
		if !c.Reveal {
			masked, err := buildURI(baseURI, creds, true)
			if err != nil {
				return err
			}
			shown = masked
		}
		fmt.Fprintln(stdout, shown)
	}
	if c.Copy {
		if err := copyToClipboard(fullURI); err != nil {
			return err
		}
		fmt.Fprintln(stderr, "Connection string copied to the clipboard.")
	}
	return nil
}

// resolved is a connection after applying command-line overrides and defaults.
type resolved struct {
	Name, URI, Profile string
}

func (r resolved) label() string {
	if r.Name != "" {
		return r.Name
	}
	return r.URI
}

// resolve combines the named connection with command-line overrides.
// Profile priority: --profile, connection, defaults.profile, $AWS_PROFILE.
func (c *CLI) resolve(cfg *Config, path string, found bool) (resolved, error) {
	r := resolved{Name: c.Connection}

	if c.Connection != "" {
		conn, ok := cfg.Connections[c.Connection]
		if !ok {
			return r, unknownConnectionError(c.Connection, cfg, path, found)
		}
		r.URI, r.Profile = conn.URI, conn.Profile
	}

	r.URI = firstNonEmpty(c.URI, r.URI)
	r.Profile = firstNonEmpty(c.Profile, r.Profile, cfg.Defaults.Profile, os.Getenv("AWS_PROFILE"))

	if r.Profile == "" {
		return r, errors.New("no AWS profile: set one on the connection, in defaults.profile, with --profile or with AWS_PROFILE")
	}
	if _, err := parseMongoURI(r.URI); err != nil {
		return r, err
	}
	return r, nil
}

func unknownConnectionError(name string, cfg *Config, path string, found bool) error {
	switch {
	case !found:
		return fmt.Errorf("unknown connection %q: no config file at %s", name, path)
	case len(cfg.Connections) == 0:
		return fmt.Errorf("unknown connection %q: %s has no connections", name, path)
	default:
		return fmt.Errorf("unknown connection %q; available: %s", name, strings.Join(cfg.names(), ", "))
	}
}

// save adds or updates a connection in the config file, creating the file if
// needed. Only the fields given on the command line change.
func (c *CLI) save(stderr io.Writer, path string) error {
	cfg, _, err := loadConfig(path, false) // a missing file is fine here
	if err != nil {
		return err
	}

	conn, exists := cfg.Connections[c.Save]
	if c.URI != "" {
		if _, err := parseMongoURI(c.URI); err != nil {
			return err
		}
		conn.URI = c.URI
	}
	if c.Profile != "" {
		conn.Profile = c.Profile
	}
	if conn.URI == "" {
		return fmt.Errorf("connection %q is new, so it needs --uri", c.Save)
	}

	if cfg.Connections == nil {
		cfg.Connections = map[string]Connection{}
	}
	cfg.Connections[c.Save] = conn
	if err := saveConfig(path, cfg); err != nil {
		return err
	}

	action := "Added"
	if exists {
		action = "Updated"
	}
	fmt.Fprintf(stderr, "%s %q in %s\n", action, c.Save, path)
	return nil
}

func (c *CLI) list(w io.Writer, path string, explicit bool) error {
	cfg, found, err := loadConfig(path, explicit)
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintf(w, "No config file yet at %s. Add a connection with --save.\n", path)
		return nil
	}
	if len(cfg.Connections) == 0 {
		fmt.Fprintf(w, "No connections in %s\n", path)
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tPROFILE\tURI")
	for _, name := range cfg.names() {
		conn := cfg.Connections[name]
		profile := conn.Profile
		if profile == "" && cfg.Defaults.Profile != "" {
			profile = cfg.Defaults.Profile + " (default)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", name, firstNonEmpty(profile, "-"), conn.URI)
	}
	return tw.Flush()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
