package account

import "testing"

func TestParseMailboxSeparatesDisplayNameFromEnvelopeAddress(t *testing.T) {
	got, err := parseMailbox("CELLS Account <accounts@CELLS.tech>")
	if err != nil {
		t.Fatal(err)
	}
	if got != "accounts@CELLS.tech" {
		t.Fatalf("envelope address=%q", got)
	}
}

func TestParseMailboxRejectsHeaderInjection(t *testing.T) {
	if _, err := parseMailbox("accounts@CELLS.tech\r\nBcc: attacker@example.test"); err == nil {
		t.Fatal("mail header injection was accepted")
	}
}
