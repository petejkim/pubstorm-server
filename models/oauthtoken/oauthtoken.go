package oauthtoken

import (
	"time"

	"github.com/jinzhu/gorm"
	"github.com/lib/pq"
	"github.com/nitrous-io/rise-server/dbconn"
	"github.com/nitrous-io/rise-server/models/user"
)

type OauthToken struct {
	ID            uint `gorm:"primary_key"`
	UserID        uint
	OauthClientID uint
	Token         string `sql:"default:encode(gen_random_bytes(64), 'hex')"`
	CreatedAt     time.Time
	DeletedAt     pq.NullTime

	User user.User // belongs to user
}

// Finds oauth token by token
func FindByToken(token string) (t *OauthToken, err error) {
	db, err := dbconn.DB()
	if err != nil {
		return nil, err
	}

	t = &OauthToken{}
	q := db.Where("token = ?", token).First(t)
	if err = q.Error; err != nil {
		if err == gorm.RecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return t, nil
}
