package secret

import (
	"crypto/ed25519"
	"testing"
	"time"
)

func TestPasswordAndTicket(t *testing.T) {
	enc, err := HashPassword("correct-horse-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPassword(enc, "correct-horse-1"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPassword(enc, "wrong-horse-111"); err == nil {
		t.Fatal("accepted a wrong password")
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := SignTicket(priv, Ticket{V: 1, JTI: "j", SID: "s", Expires: time.Now().Add(time.Minute).Unix(), ServerID: "srv"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyTicket(pub, raw, time.Now())
	if err != nil || got.SID != "s" {
		t.Fatal(err, got)
	}
	_, other, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyTicket(other.Public().(ed25519.PublicKey), raw, time.Now()); err == nil {
		t.Fatal("other key verified the ticket")
	}
}
