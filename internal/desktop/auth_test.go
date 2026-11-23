package desktop

import "testing"

func TestChangedFingerprintIsRejected(t *testing.T) {
	if err := CheckPeer("aa", "aa", "noise", "noise"); err != nil {
		t.Fatal(err)
	}
	if err := CheckPeer("aa", "bb", "noise", "noise"); err != ErrIdentity {
		t.Fatalf("changed key was accepted: %v", err)
	}
	if err := CheckPeer("aa", "aa", "noise", "other"); err != ErrIdentity {
		t.Fatalf("ticket noise mismatch accepted: %v", err)
	}
}
