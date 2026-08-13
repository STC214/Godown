package httpdownload

import "testing"

func TestParseHeaders(t *testing.T) {
	headers, err := ParseHeaders("User-Agent: GD3\r\nReferer: https://example.com/\n\nCookie: a=b")
	if err != nil {
		t.Fatal(err)
	}
	if headers["User-Agent"] != "GD3" {
		t.Fatalf("unexpected user-agent: %#v", headers)
	}
	if headers["Referer"] != "https://example.com/" {
		t.Fatalf("unexpected referer: %#v", headers)
	}
	if headers["Cookie"] != "a=b" {
		t.Fatalf("unexpected cookie: %#v", headers)
	}
}

func TestParseHeadersRejectsInvalidLine(t *testing.T) {
	if _, err := ParseHeaders("Broken"); err == nil {
		t.Fatal("expected invalid header line error")
	}
}

func TestNormalizeCookies(t *testing.T) {
	got := NormalizeCookies("a=b; c = d\r\nbroken\nempty=\nctl=bad\x01value")
	if got != "a=b; c=d" {
		t.Fatalf("unexpected normalized cookies: %q", got)
	}
}

func TestMergeCookiesAddsCookieHeader(t *testing.T) {
	headers := map[string]string{"User-Agent": "GD3"}
	got := MergeCookies(headers, "a=b\nc=d")
	if got["Cookie"] != "a=b; c=d" {
		t.Fatalf("unexpected cookie header: %#v", got)
	}
}

func TestMergeCookiesAppendsExistingCookieHeaderCaseInsensitive(t *testing.T) {
	headers := map[string]string{"cookie": "session=old"}
	got := MergeCookies(headers, "a=b")
	if got["cookie"] != "session=old; a=b" {
		t.Fatalf("unexpected merged cookie header: %#v", got)
	}
}
