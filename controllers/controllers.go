package controllers

import (
	"github.com/nitrous-io/rise-server/models/oauthtoken"
	"github.com/nitrous-io/rise-server/models/user"

	"github.com/gin-gonic/gin"
)

const (
	CurrentTokenKey = "current_token"
	CurrentUserKey  = "current_user"
)

func CurrentToken(c *gin.Context) *oauthtoken.OauthToken {
	ti, exists := c.Get(CurrentTokenKey)
	if ti == nil || !exists {
		return nil
	}

	t, ok := ti.(*oauthtoken.OauthToken)
	if !ok {
		return nil
	}
	return t
}

func CurrentUser(c *gin.Context) *user.User {
	ui, exists := c.Get(CurrentUserKey)
	if ui == nil || !exists {
		return nil
	}

	u, ok := ui.(*user.User)
	if !ok {
		return nil
	}
	return u
}
