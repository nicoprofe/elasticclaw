package hub

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Signed, stateless setup state.
//
// The pending flow used to live in an in-memory map, which meant restarting the
// hub — including every upgrade — silently invalidated any setup link already
// handed out. A user who created the App on GitHub in that window got it created
// for real, while the callback was rejected and the App ID and private key were
// dropped. The key is returned by GitHub exactly once, so it could not be
// recovered: the App had to be deleted and the flow repeated.
//
// Signing the workspace and an expiry into the value removes the server-side
// state entirely, so a restart no longer breaks a flow in progress.
//
// Replay is not a concern: a replayed ticket only mints a fresh redirect, and a
// replayed callback re-presents a conversion code that GitHub has already spent.

// manifestStateValidity is how long a setup link remains usable.
const manifestStateValidity = 30 * time.Minute

// signManifestState encodes "workspace|expiry" with an HMAC the hub can verify
// later. The secret is the hub's own, which persists in hub.yaml.
func signManifestState(secret, workspace string) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("hub has no token to sign setup state with")
	}
	payload := fmt.Sprintf("%s|%d", workspace, time.Now().Add(manifestStateValidity).Unix())
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("elasticclaw:app-manifest:" + encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return encoded + "." + sig, nil
}

// verifyManifestState checks the signature and expiry, returning the workspace
// the flow was started for.
func verifyManifestState(secret, state string) (string, bool) {
	if secret == "" || state == "" {
		return "", false
	}
	encoded, sig, found := strings.Cut(state, ".")
	if !found {
		return "", false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("elasticclaw:app-manifest:" + encoded))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	// Constant time: the signature is the only thing preventing someone from
	// choosing which workspace a callback writes to.
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", false
	}

	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", false
	}
	workspace, expiryStr, found := strings.Cut(string(raw), "|")
	if !found {
		return "", false
	}
	expiry, err := strconv.ParseInt(expiryStr, 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return "", false
	}
	return workspace, true
}
