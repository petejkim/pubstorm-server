package config

import (
	"os"
	"sync"

	"github.com/jinzhu/gorm"
	_ "github.com/lib/pq"
)

var (
	db     *gorm.DB
	dbLock sync.Mutex
)

func DB() (*gorm.DB, error) {
	dbLock.Lock()
	defer dbLock.Unlock()
	if db == nil {
		d, err := gorm.Open("postgres", os.Getenv("POSTGRES_URL"))
		if err != nil {
			return nil, err
		}
		db = &d
	}
	return db, nil
}
