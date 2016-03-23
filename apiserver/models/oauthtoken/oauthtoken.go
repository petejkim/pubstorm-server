package oauthtoken

import (
	"time"

	"github.com/jinzhu/gorm"
	"github.com/lib/pq"
)

type OauthToken struct {
	ID            uint `gorm:"primary_key"`
	UserID        uint
	OauthClientID uint
	Token         string `sql:"default:encode(gen_random_bytes(64), 'hex')"`
	CreatedAt     time.Time
	DeletedAt     pq.NullTime
}

// Finds oauth token by token
func FindByToken(db *gorm.DB, token string) (t *OauthToken, err error) {
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
