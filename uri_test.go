package main

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

var testCreds = &Credentials{
	AccessKeyID:     "ASIAEXAMPLE",
	SecretAccessKey: "abc/def+ghi=",
	SessionToken:    "IQo/tok+en==",
}

func TestBuildURI(t *testing.T) {
	tests := []struct {
		name, base, want string
	}{
		{
			name: "srv host only",
			base: "mongodb+srv://cluster.xmbso.mongodb.net",
			want: "mongodb+srv://ASIAEXAMPLE:abc%2Fdef%2Bghi%3D@cluster.xmbso.mongodb.net/" +
				"?authMechanism=MONGODB-AWS&authSource=%24external&authMechanismProperties=AWS_SESSION_TOKEN:IQo%2Ftok%2Ben%3D%3D",
		},
		{
			name: "database and existing parameters kept in order",
			base: "mongodb+srv://cluster.net/mydb?appName=me&readPreference=secondary",
			want: "mongodb+srv://ASIAEXAMPLE:abc%2Fdef%2Bghi%3D@cluster.net/mydb" +
				"?appName=me&readPreference=secondary&authMechanism=MONGODB-AWS&authSource=%24external" +
				"&authMechanismProperties=AWS_SESSION_TOKEN:IQo%2Ftok%2Ben%3D%3D",
		},
		{
			name: "existing auth parameters merged, old token replaced",
			base: "mongodb://h1:27017,h2:27017/?authMechanism=MONGODB-AWS&authSource=$external" +
				"&authMechanismProperties=SERVICE_NAME:x,AWS_SESSION_TOKEN:old",
			want: "mongodb://ASIAEXAMPLE:abc%2Fdef%2Bghi%3D@h1:27017,h2:27017/" +
				"?authSource=$external&authMechanism=MONGODB-AWS" +
				"&authMechanismProperties=SERVICE_NAME:x,AWS_SESSION_TOKEN:IQo%2Ftok%2Ben%3D%3D",
		},
		{
			name: "trailing slash and question mark",
			base: "mongodb+srv://cluster.net/?",
			want: "mongodb+srv://ASIAEXAMPLE:abc%2Fdef%2Bghi%3D@cluster.net/" +
				"?authMechanism=MONGODB-AWS&authSource=%24external&authMechanismProperties=AWS_SESSION_TOKEN:IQo%2Ftok%2Ben%3D%3D",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildURI(tt.base, testCreds, false)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("\ngot  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestBuildURIRoundTrip(t *testing.T) {
	got, err := buildURI("mongodb+srv://cluster.net", testCreds, false)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if pw, _ := u.User.Password(); pw != testCreds.SecretAccessKey {
		t.Errorf("secret decoded to %q", pw)
	}
	props := u.Query().Get("authMechanismProperties")
	if props != "AWS_SESSION_TOKEN:"+testCreds.SessionToken {
		t.Errorf("token decoded to %q", props)
	}
}

func TestBuildURIMasked(t *testing.T) {
	got, err := buildURI("mongodb+srv://cluster.net", testCreds, true)
	if err != nil {
		t.Fatal(err)
	}
	want := "mongodb+srv://ASIAEXAMPLE:****@cluster.net/?authMechanism=MONGODB-AWS&authSource=%24external&authMechanismProperties=AWS_SESSION_TOKEN:****"
	if got != want {
		t.Errorf("\ngot  %s\nwant %s", got, want)
	}
}

func TestBuildURIWithoutCredentials(t *testing.T) {
	got, err := buildURI("mongodb+srv://cluster.net", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "mongodb+srv://cluster.net/?authMechanism=MONGODB-AWS&authSource=%24external"; got != want {
		t.Errorf("\ngot  %s\nwant %s", got, want)
	}
}

func TestBuildURILongTermKeys(t *testing.T) {
	got, err := buildURI("mongodb+srv://cluster.net", &Credentials{AccessKeyID: "AKIA", SecretAccessKey: "s"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "AWS_SESSION_TOKEN") {
		t.Errorf("unexpected session token in %s", got)
	}
}

func TestBuildURIErrors(t *testing.T) {
	for _, base := range []string{
		"https://cluster.net",
		"cluster.net",
		"mongodb+srv://user:pw@cluster.net",
		"mongodb+srv://",
		"mongodb+srv://cluster.net/?authMechanism=SCRAM-SHA-256",
		"mongodb+srv://cluster.net/?authSource=admin",
	} {
		if _, err := buildURI(base, testCreds, false); err == nil {
			t.Errorf("%s: expected an error", base)
		}
	}
}

func TestDescribeExpiry(t *testing.T) {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	got := describeExpiry("2026-10-08T11:59:40+00:00", now)
	if !strings.HasSuffix(got, "(3h0m)") {
		t.Errorf("got %q", got)
	}
	if got := describeExpiry("", now); got != "" {
		t.Errorf("got %q for empty expiration", got)
	}
}
