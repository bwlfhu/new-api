package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func PDEPProviderAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
		fields := strings.Fields(authHeader)
		if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || strings.TrimSpace(fields[1]) == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "未提供有效的 Bearer 鉴权信息",
			})
			c.Abort()
			return
		}

		expectedSecret := common.GetPDEPProviderSecret()
		if expectedSecret == "" || subtle.ConstantTimeCompare([]byte(fields[1]), []byte(expectedSecret)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "PDEP Provider 鉴权失败",
			})
			c.Abort()
			return
		}

		ownerUserID := common.GetPDEPProviderOwnerUserID()
		if ownerUserID <= 0 {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "PDEP Provider owner 未配置",
			})
			c.Abort()
			return
		}

		ownerUser, err := model.GetUserById(ownerUserID, false)
		if err != nil || ownerUser == nil || ownerUser.Status != common.UserStatusEnabled {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "PDEP Provider owner 用户不可用",
			})
			c.Abort()
			return
		}

		c.Set("id", ownerUserID)
		c.Next()
	}
}
