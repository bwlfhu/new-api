package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func recordManageAudit(c *gin.Context, action string, params map[string]interface{}) {
	adminInfo := map[string]interface{}{
		"admin_id":       c.GetInt("id"),
		"admin_username": c.GetString("username"),
		"admin_role":     c.GetInt("role"),
	}
	if len(params) > 0 {
		adminInfo["params"] = params
	}
	model.RecordLogWithAdminInfo(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("admin action: %s", action), adminInfo)
}
