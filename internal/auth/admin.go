package auth

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/madeinoz67/go-rag/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

// DefaultAdminUsername is the single v1 administrator's username (spec 045
// US6 — single-operator vault; multi-user is PRD §2.2 out-of-scope).
const DefaultAdminUsername = "admin"

// bcryptCost is the bcrypt cost used for admin passwords. 12 is the current
// floor that keeps naive GPU guessing materially more expensive than login
// rate (a single-operator loopback vault already throttles via the network
// bypass — US5 — but the hash must stand on its own if the vault is exposed).
const bcryptCost = 12

// ErrUnknownAdmin is returned when no admin user bears the username, and
// ErrBadPassword on a bcrypt mismatch. Callers map both to an identical 401 +
// audit.AuthFailEvent so a probe cannot distinguish "no such user" from "wrong
// password" (no username enumeration).
var (
	ErrUnknownAdmin  = errors.New("unknown admin user")
	ErrBadPassword   = errors.New("bad password")
	ErrAdminExists   = errors.New("admin user already exists")
	ErrNoAdmin       = errors.New("no admin user is bootstrapped")
)

// AdminUser is the bootstrap administrator. The password is never stored in
// plaintext — only the bcrypt hash (PassHash). Pebble key:
// storage.PrefixAuthAdmin || Username.
type AdminUser struct {
	Username  string    `json:"username"`
	PassHash  []byte    `json:"pass_hash"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateAdmin upserts the admin user with a fresh bcrypt hash of password.
// Use it for both first bootstrap and password rotation (spec 045 US6:
// GORAG_ADMIN_PASSWORD on a subsequent start rotates the existing admin).
func CreateAdmin(s *Store, username, password string) (AdminUser, error) {
	if username == "" {
		username = DefaultAdminUsername
	}
	if password == "" {
		return AdminUser{}, errors.New("admin password must not be empty")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return AdminUser{}, err
	}
	admin := AdminUser{
		Username:  username,
		PassHash:  hash,
		CreatedAt: time.Now().UTC(),
	}
	val, err := json.Marshal(admin)
	if err != nil {
		return AdminUser{}, err
	}
	if err := s.db.SetWithPrefix(storage.PrefixAuthAdmin, []byte(username), val); err != nil {
		return AdminUser{}, err
	}
	return admin, nil
}

// AdminExists reports whether an admin user is bootstrapped under username.
func AdminExists(s *Store, username string) (bool, error) {
	if username == "" {
		username = DefaultAdminUsername
	}
	_, ok, err := s.db.GetWithPrefix(storage.PrefixAuthAdmin, []byte(username))
	return ok, err
}

// GetAdmin loads the admin user (used by login before password verification).
func GetAdmin(s *Store, username string) (AdminUser, error) {
	if username == "" {
		username = DefaultAdminUsername
	}
	val, ok, err := s.db.GetWithPrefix(storage.PrefixAuthAdmin, []byte(username))
	if err != nil {
		return AdminUser{}, err
	}
	if !ok {
		return AdminUser{}, ErrUnknownAdmin
	}
	var admin AdminUser
	if err := json.Unmarshal(val, &admin); err != nil {
		return AdminUser{}, err
	}
	return admin, nil
}

// VerifyPassword resolves username+password to the admin user. A missing user
// and a wrong password both return errors the caller MUST collapse into the
// same 401 (no username enumeration). bcrypt.CompareHashAndPassword is
// constant-time.
func VerifyPassword(s *Store, username, password string) (AdminUser, error) {
	admin, err := GetAdmin(s, username)
	if err != nil {
		// Still run a dummy bcrypt compare so the missing-user path takes the
		// same wall-clock as the present-user path (timing-neutral).
		_ = bcrypt.CompareHashAndPassword(stubBcryptHash, []byte(password))
		return AdminUser{}, ErrUnknownAdmin
	}
	if err := bcrypt.CompareHashAndPassword(admin.PassHash, []byte(password)); err != nil {
		return AdminUser{}, ErrBadPassword
	}
	return admin, nil
}

// stubBcryptHash is a valid bcrypt hash of a random value used only to keep the
// missing-user timing envelope aligned with the present-user envelope. It is
// never compared for access — a real admin lookup must precede any success.
var stubBcryptHash = mustStubHash()

func mustStubHash() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("timing-decoy-only"), bcryptCost)
	if err != nil {
		// bcrypt.GenerateFromPassword only fails on bad cost; 12 is valid.
		panic(err)
	}
	return h
}
