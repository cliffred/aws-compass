package main

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	awsMechanism = "MONGODB-AWS"
	mask         = "****"
)

// buildURI adds AWS IAM authentication to a connection string:
//
//	mongodb+srv://host/db?appName=x
//	→ mongodb+srv://KEY:SECRET@host/db?appName=x&authMechanism=MONGODB-AWS
//	  &authSource=%24external&authMechanismProperties=AWS_SESSION_TOKEN:TOKEN
//
// With creds nil it only adds the mechanism and source (for mongosh, which
// gets the credentials itself). With maskSecrets the secret and session token
// are shown as ****.
func buildURI(base string, creds *Credentials, maskSecrets bool) (string, error) {
	u, err := parseMongoURI(base)
	if err != nil {
		return "", err
	}

	userinfo, token := "", ""
	if creds != nil {
		secret := encode(creds.SecretAccessKey)
		token = encode(creds.SessionToken)
		if maskSecrets {
			secret = mask
			if token != "" {
				token = mask
			}
		}
		userinfo = encode(creds.AccessKeyID) + ":" + secret
	}

	if err := u.addAWSAuth(token); err != nil {
		return "", err
	}
	return u.String(userinfo), nil
}

// mongoURI is a MongoDB connection string split into its parts. It's parsed
// by hand because net/url rejects seed lists like "h1:27017,h2:27017".
type mongoURI struct {
	scheme string // "mongodb" or "mongodb+srv"
	hosts  string
	path   string // database, without the leading slash
	params []param
}

// param is a query parameter with its value still percent-encoded as written,
// so parameters we don't touch come out exactly as they went in.
type param struct {
	key      string
	rawValue string
}

// parseMongoURI splits scheme://hosts/path?query. It rejects URIs that
// already contain credentials.
func parseMongoURI(s string) (*mongoURI, error) {
	s = strings.TrimSpace(s)
	scheme, rest, ok := strings.Cut(s, "://")
	if !ok || (scheme != "mongodb" && scheme != "mongodb+srv") {
		return nil, fmt.Errorf("uri %q must start with mongodb:// or mongodb+srv://", s)
	}
	rest, _, _ = strings.Cut(rest, "#") // drop a fragment

	u := &mongoURI{scheme: scheme, hosts: rest}
	tail := ""
	if i := strings.IndexAny(rest, "/?"); i >= 0 {
		u.hosts, tail = rest[:i], rest[i:]
	}
	if u.hosts == "" {
		return nil, fmt.Errorf("uri %q has no host", s)
	}
	if strings.Contains(u.hosts, "@") {
		return nil, fmt.Errorf("uri must not contain credentials; aws-compass adds them")
	}

	path, query, _ := strings.Cut(strings.TrimPrefix(tail, "/"), "?")
	u.path = path
	for _, part := range strings.Split(query, "&") {
		if part != "" {
			key, value, _ := strings.Cut(part, "=")
			u.params = append(u.params, param{key, value})
		}
	}
	return u, nil
}

// addAWSAuth sets authMechanism=MONGODB-AWS, authSource=$external (unless
// already present) and the session token, keeping other parameters in their
// original order. encodedToken is already percent-encoded or masked; empty
// means long-term keys without a token.
func (u *mongoURI) addAWSAuth(encodedToken string) error {
	var kept []param
	var props []string // authMechanismProperties entries other than the token
	hasAuthSource := false

	for _, p := range u.params {
		value, err := url.QueryUnescape(p.rawValue)
		if err != nil {
			return fmt.Errorf("uri parameter %s: %w", p.key, err)
		}

		// MongoDB option names are case-insensitive.
		switch strings.ToLower(p.key) {
		case "authmechanism":
			if !strings.EqualFold(value, awsMechanism) {
				return fmt.Errorf("uri sets authMechanism=%s; aws-compass only supports %s", value, awsMechanism)
			}
			// added again below
		case "authsource":
			if value != "$external" {
				return fmt.Errorf("uri sets authSource=%s; %s requires $external", value, awsMechanism)
			}
			hasAuthSource = true
			kept = append(kept, p)
		case "authmechanismproperties":
			props = append(props, propertiesWithoutToken(value)...)
			// added again below, with the new token
		default:
			kept = append(kept, p)
		}
	}

	kept = append(kept, param{"authMechanism", awsMechanism})
	if !hasAuthSource {
		kept = append(kept, param{"authSource", "%24external"})
	}
	if encodedToken != "" {
		props = append(props, "AWS_SESSION_TOKEN:"+encodedToken)
	}
	if len(props) > 0 {
		kept = append(kept, param{"authMechanismProperties", strings.Join(props, ",")})
	}

	u.params = kept
	return nil
}

// propertiesWithoutToken splits "A:1,AWS_SESSION_TOKEN:x" into encoded
// key:value entries, dropping any old session token.
func propertiesWithoutToken(value string) []string {
	var props []string
	for _, prop := range strings.Split(value, ",") {
		key, val, _ := strings.Cut(prop, ":")
		if key != "" && !strings.EqualFold(key, "AWS_SESSION_TOKEN") {
			props = append(props, encode(key)+":"+encode(val))
		}
	}
	return props
}

// String renders the URI. userinfo is "key:secret", already encoded, or empty.
func (u *mongoURI) String(userinfo string) string {
	var b strings.Builder
	b.WriteString(u.scheme + "://")
	if userinfo != "" {
		b.WriteString(userinfo + "@")
	}
	b.WriteString(u.hosts + "/" + u.path)
	for i, p := range u.params {
		separator := "&"
		if i == 0 {
			separator = "?"
		}
		b.WriteString(separator + p.key + "=" + p.rawValue)
	}
	return b.String()
}

// encode percent-encodes everything except RFC 3986 unreserved characters,
// so secrets and tokens containing '/', '+' or '=' survive anywhere in a URI.
func encode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreserved(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func isUnreserved(c byte) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' ||
		c == '-' || c == '.' || c == '_' || c == '~'
}
