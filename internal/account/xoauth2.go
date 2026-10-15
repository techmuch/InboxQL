package account

import (
	"errors"
	"fmt"
)

// XOAuth2 is the SASL mechanism Gmail uses for an OAuth access token, over
// IMAP and SMTP alike. go-sasl does not carry it, and it is one message.
//
// https://developers.google.com/gmail/imap/xoauth2-protocol
type xoauth2 struct{ user, token string }

// XOAuth2 returns a SASL client signing in as user with an access token.
func XOAuth2(user, token string) interface {
	Start() (mech string, ir []byte, err error)
	Next(challenge []byte) (response []byte, err error)
} {
	return &xoauth2{user: user, token: token}
}

func (x *xoauth2) Start() (string, []byte, error) {
	return "XOAUTH2", []byte(fmt.Sprintf("user=%s\x01auth=Bearer %s\x01\x01", x.user, x.token)), nil
}

// Next answers the one challenge Google sends on failure — a JSON error —
// with the empty response the protocol requires, then reports the failure.
func (x *xoauth2) Next(challenge []byte) ([]byte, error) {
	if len(challenge) > 0 {
		return []byte{}, errors.New("authentication failed: Google refused the access token: " + string(challenge))
	}
	return []byte{}, nil
}
