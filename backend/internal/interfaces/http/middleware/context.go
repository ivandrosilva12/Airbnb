package middleware

import (
	"github.com/airhost/backend/internal/domain/user"
	"github.com/gin-gonic/gin"
)

const currentUserKey = "currentUser"

// SetCurrentUser stores the authenticated user on the request context.
func SetCurrentUser(c *gin.Context, u *user.User) {
	c.Set(currentUserKey, u)
}

// CurrentUser retrieves the authenticated user, if any.
func CurrentUser(c *gin.Context) (*user.User, bool) {
	v, ok := c.Get(currentUserKey)
	if !ok {
		return nil, false
	}
	u, ok := v.(*user.User)
	return u, ok
}
