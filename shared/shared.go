package shared

import (
	"os"
	"strconv"

	log "github.com/Sirupsen/logrus"
)

var (
	DefaultDomain        = os.Getenv("DEFAULT_DOMAIN") // default domain (e.g. rise.cloud)
	MaxDomainsPerProject = 5                           // MAX_DOMAINS - max # of custom domains per project
)

func init() {
	riseEnv := os.Getenv("RISE_ENV")
	if riseEnv == "" {
		riseEnv = "development"
	}

	if DefaultDomain == "" {
		DefaultDomain = "risecloud.dev"
	}

	if maxDomainsEnv := os.Getenv("MAX_DOMAINS"); maxDomainsEnv != "" {
		n, err := strconv.Atoi(maxDomainsEnv)
		if err != nil {
			log.Warn("Ignoring MAX_DOMAINS, not a valid numeric value!")
		} else {
			MaxDomainsPerProject = n
		}
	}
}
