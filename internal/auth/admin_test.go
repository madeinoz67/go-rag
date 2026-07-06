package auth

import (
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/storage"
)

func TestCreateAdmin_BcryptRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if _, err := CreateAdmin(s, DefaultAdminUsername, "correct-horse-battery"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	if _, err := VerifyPassword(s, DefaultAdminUsername, "correct-horse-battery"); err != nil {
		t.Fatalf("VerifyPassword(correct): %v", err)
	}
}

func TestVerifyPassword_BadPassword(t *testing.T) {
	s := newTestStore(t)
	if _, err := CreateAdmin(s, DefaultAdminUsername, "right"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	if _, err := VerifyPassword(s, DefaultAdminUsername, "wrong"); err != ErrBadPassword {
		t.Fatalf("want ErrBadPassword, got %v", err)
	}
}

func TestVerifyPassword_UnknownUserNoEnumerate(t *testing.T) {
	s := newTestStore(t)
	// No admin created — a missing user returns ErrUnknownAdmin (the caller
	// collapses this with ErrBadPassword into a 401 so the probe cannot tell
	// "no such user" from "wrong password").
	if _, err := VerifyPassword(s, DefaultAdminUsername, "anything"); err != ErrUnknownAdmin {
		t.Fatalf("want ErrUnknownAdmin, got %v", err)
	}
}

func TestAdminExists_Flips(t *testing.T) {
	s := newTestStore(t)
	got, err := AdminExists(s, DefaultAdminUsername)
	if err != nil {
		t.Fatalf("AdminExists(pre): %v", err)
	}
	if got {
		t.Fatal("AdminExists = true before create")
	}
	if _, err := CreateAdmin(s, DefaultAdminUsername, "pw"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	got, err = AdminExists(s, DefaultAdminUsername)
	if err != nil {
		t.Fatalf("AdminExists(post): %v", err)
	}
	if !got {
		t.Fatal("AdminExists = false after create")
	}
}

func TestCreateAdmin_RotationInvalidatesOldPassword(t *testing.T) {
	s := newTestStore(t)
	if _, err := CreateAdmin(s, DefaultAdminUsername, "old-pw"); err != nil {
		t.Fatalf("CreateAdmin(old): %v", err)
	}
	if _, err := CreateAdmin(s, DefaultAdminUsername, "new-pw"); err != nil {
		t.Fatalf("CreateAdmin(new): %v", err)
	}
	if _, err := VerifyPassword(s, DefaultAdminUsername, "old-pw"); err != ErrBadPassword {
		t.Fatalf("old password after rotation: want ErrBadPassword, got %v", err)
	}
	if _, err := VerifyPassword(s, DefaultAdminUsername, "new-pw"); err != nil {
		t.Fatalf("new password after rotation: %v", err)
	}
}

func TestCreateAdmin_NoInsecureDefault(t *testing.T) {
	s := newTestStore(t)
	// Empty password is rejected — no implicit "password"/"root"/"admin" default.
	if _, err := CreateAdmin(s, DefaultAdminUsername, ""); err == nil {
		t.Fatal("CreateAdmin('') succeeded; want error")
	}
}

func TestPassHashNeverPlaintext(t *testing.T) {
	s := newTestStore(t)
	pw := "super-secret-value"
	if _, err := CreateAdmin(s, DefaultAdminUsername, pw); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	if err := s.db.PrefixScanByte(storage.PrefixAuthAdmin, func(_, val []byte) bool {
		if strings.Contains(string(val), pw) {
			t.Fatalf("plaintext password leaked into stored admin value: %s", val)
		}
		return true
	}); err != nil {
		t.Fatalf("scan: %v", err)
	}
}
