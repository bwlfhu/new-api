package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func pdepProviderNotImplemented(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"success": false,
		"message": "PDEP Provider 接口暂未实现",
	})
}

func PDEPProviderGetTokens(c *gin.Context) {
	pdepProviderNotImplemented(c)
}

func PDEPProviderCreateToken(c *gin.Context) {
	pdepProviderNotImplemented(c)
}

func PDEPProviderDeleteToken(c *gin.Context) {
	pdepProviderNotImplemented(c)
}

func PDEPProviderGetAggregatedTokens(c *gin.Context) {
	pdepProviderNotImplemented(c)
}
