package dns

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicalValueAcceptsEveryTypeInZoneFileForm(t *testing.T) {
	cases := []struct{ typ, in, want string }{
		{"A", "198.51.100.10", "198.51.100.10"},
		{"AAAA", "2001:db8::10", "2001:db8::10"},
		{"CNAME", "Target.Example.com.", "target.example.com"},
		{"NS", "ns1.example.net.", "ns1.example.net"},
		{"TXT", "v=spf1 include:example.net ~all", "v=spf1 include:example.net ~all"},
		{"MX", "10  Mail.Example.com.", "10 mail.example.com"},
		{"SRV", "10 5 5060 sip.example.com.", "10 5 5060 sip.example.com"},
		{"CAA", "0 issue ca.example.net", `0 issue "ca.example.net"`},
		{"CAA", `0 ISSUE "ca.example.net"`, `0 issue "ca.example.net"`},
	}
	for _, tc := range cases {
		got, err := CanonicalValue(tc.typ, tc.in)
		if err != nil {
			t.Errorf("%s %q: %v", tc.typ, tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s %q: got %q, want %q", tc.typ, tc.in, got, tc.want)
		}
	}
}

func TestCanonicalValueRefusesAValueThatDoesNotFitItsType(t *testing.T) {
	cases := []struct{ typ, in, mention string }{
		{"A", "example.com", "IPv4"},
		{"A", "2001:db8::10", "IPv4"},
		{"AAAA", "198.51.100.10", "IPv6"},
		{"CNAME", "a b", "<host>"},
		{"MX", "mail.example.com", "<priority> <host>"},
		{"MX", "70000 mail.example.com", "65535"},
		{"MX", "ten mail.example.com", "<priority> <host>"},
		{"SRV", "10 5 sip.example.com", "<priority> <weight> <port> <target>"},
		{"SRV", "10 5 99999 sip.example.com", "65535"},
		{"CAA", "0 issue", "<flags> <tag> <value>"},
		{"CAA", "300 issue ca.example.net", "255"},
		{"CAA", "0 allow ca.example.net", "issue, issuewild, iodef"},
		{"TXT", "", "empty"},
	}
	for _, tc := range cases {
		_, err := CanonicalValue(tc.typ, tc.in)
		if !errors.Is(err, ErrBadValue) {
			t.Errorf("%s %q: got %v, want ErrBadValue", tc.typ, tc.in, err)
			continue
		}
		if !strings.Contains(err.Error(), tc.mention) {
			t.Errorf("%s %q: the error does not say %q: %v", tc.typ, tc.in, tc.mention, err)
		}
	}
}

func TestCanonicalValueKeepsATXTValueVerbatim(t *testing.T) {
	in := `v=DKIM1; k=rsa; p=MIGf  MA0G`
	got, err := CanonicalValue("txt", in)
	if err != nil {
		t.Fatal(err)
	}
	if got != in {
		t.Fatalf("got %q, want it untouched", got)
	}
}

func TestCanonicalValueRefusesATypeItDoesNotKnow(t *testing.T) {
	_, err := CanonicalValue("SOA", "ns1.example.com hostmaster.example.com 1 2 3 4 5")
	if !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("got %v", err)
	}
}
