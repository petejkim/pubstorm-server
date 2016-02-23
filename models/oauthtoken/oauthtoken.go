package oauthtoken

import (
	"time"

	"github.com/nitrous-io/rise-server/models/user"
)

type OauthToken struct {
	ID            uint `gorm:"primary_key"`
	UserID        uint
	OauthClientID uint
	Token         string `sql:"default:encode(gen_random_bytes(64), 'hex')"`
	CreatedAt     time.Time

	User user.User // belongs to user
}
