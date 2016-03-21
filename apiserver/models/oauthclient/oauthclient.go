package oauthclient

import (
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
)

type OauthClient struct {
	gorm.Model

	ClientID     string `sql:"default:encode(gen_random_bytes(16), 'hex')"`
	ClientSecret string `sql:"default:encode(gen_random_bytes(64), 'hex')"`
	Email        string
	Name         string
	Organization string
}

// Checks client id and client secret and return client if credentials are valid
func Authenticate(clientID, clientSecret string) (c *OauthClient, err error) {
	db, err := dbconn.DB()
	if err != nil {
		return nil, err
	}

	c = &OauthClient{}
	if err = db.Where(
		"client_id = ? AND client_secret = ?",
		clientID, clientSecret).First(c).Error; err != nil {
		// don't treat record not found as error
		if err == gorm.RecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return c, err
}
