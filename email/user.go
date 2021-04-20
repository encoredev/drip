package email

import (
	"context"
	"database/sql"
	"encoding/base64"
	"log"

	"encore.dev/storage/sqldb"
	"github.com/gorilla/securecookie"
)

type UnsubscribeParams struct {
	Token string
}

//encore:api auth
func Unsubscribe(ctx context.Context, params *UnsubscribeParams) error {
	email, emailID, err := decodeEmailToken(params.Token)
	if err != nil {
		return err
	}
	_, err = sqldb.Exec(ctx, `
		UPDATE "user" SET optin = false, optin_changed = NOW()
		WHERE email = $1 AND optin
	`, email)
	if err != nil {
		return err
	}

	_, err = sqldb.Exec(ctx, `
		INSERT INTO "unsubscribe_event" (email, email_id, event_time)
		VALUES ($1, $2, NOW())
	`, email, emailID)
	return err
}

func ensureUserCreated(ctx context.Context, email string) error {
	_, err := sqldb.Exec(ctx, `
		INSERT INTO "user" (email, optin, optin_changed)
		VALUES ($1, true, NOW())
		ON CONFLICT (email) DO NOTHING
	`, email)
	return err
}

func isOptedIn(ctx context.Context, email string) (bool, error) {
	var status bool
	err := sqldb.QueryRow(ctx, `
		SELECT optin FROM "user" WHERE email = $1
	`, email).Scan(&status)
	if err == sql.ErrNoRows {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return status, nil
}

type emailTokenData struct {
	Email   string
	EmailID int64
}

func encodeEmailToken(email string, emailID int64) (string, error) {
	return tokenCookie.Encode("token", emailTokenData{Email: email, EmailID: emailID})
}

func decodeEmailToken(token string) (email string, emailID int64, err error) {
	var data emailTokenData
	err = tokenCookie.Decode("token", token, &data)
	return data.Email, data.EmailID, nil
}

var tokenCookie = func() *securecookie.SecureCookie {
	hashKey, err := base64.RawURLEncoding.DecodeString(secrets.TokenHashKey)
	if err != nil {
		log.Fatalln("bad TokenHashKey:", err)
	}
	return securecookie.New(hashKey, nil)
}()
