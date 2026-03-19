package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func pdepProviderNotImplemented(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"success": false,
		"message": "PDEP Provider 接口暂未实现",
	})
}

func PDEPProviderGetTokens(c *gin.Context) {
	ownerID := c.GetInt("id")
	items, err := model.ListPDEPTokens(ownerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "internal error",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items": items,
	})
}

func PDEPProviderCreateToken(c *gin.Context) {
	var request struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid request body",
		})
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "name is required",
		})
		return
	}

	ownerID := c.GetInt("id")
	result, err := model.CreatePDEPToken(ownerID, name)
	if errors.Is(err, model.ErrPDEPTokenNameConflict) {
		c.JSON(http.StatusConflict, gin.H{
			"message": "token name conflict",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "internal error",
		})
		return
	}
	c.JSON(http.StatusOK, result)
}

func PDEPProviderDeleteToken(c *gin.Context) {
	tokenID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid id",
		})
		return
	}

	ownerID := c.GetInt("id")
	err = model.DeletePDEPToken(ownerID, tokenID)
	if errors.Is(err, model.ErrPDEPForbiddenToken) {
		c.JSON(http.StatusForbidden, gin.H{
			"message": "forbidden",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "internal error",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

func PDEPProviderGetAggregatedTokens(c *gin.Context) {
	pdepProviderNotImplemented(c)
}
