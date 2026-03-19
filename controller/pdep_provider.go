package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	sourceID := c.Query("sourceId")
	if sourceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid sourceId",
		})
		return
	}

	startUTC, err := parsePDEPQueryUTC(c.Query("startTime"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid time range",
		})
		return
	}
	endUTC, err := parsePDEPQueryUTC(c.Query("endTime"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid time range",
		})
		return
	}
	if !startUTC.Before(endUTC) {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid time range",
		})
		return
	}
	if !isAlignedPDEPTenMinuteUTC(startUTC) || !isAlignedPDEPTenMinuteUTC(endUTC) {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid time range",
		})
		return
	}

	ownerID := c.GetInt("id")
	buckets, err := model.GetPDEPTokenAggregated(ownerID, sourceID, startUTC, endUTC)
	if errors.Is(err, model.ErrPDEPInvalidSourceID) {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid sourceId",
		})
		return
	}
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
		"buckets": buckets,
	})
}

func parsePDEPQueryUTC(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, errors.New("invalid utc timestamp")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	_, offset := parsed.Zone()
	if offset != 0 {
		return time.Time{}, errors.New("invalid utc timestamp")
	}
	return parsed.UTC(), nil
}

func isAlignedPDEPTenMinuteUTC(ts time.Time) bool {
	utc := ts.UTC()
	return utc.Minute()%10 == 0 && utc.Second() == 0 && utc.Nanosecond() == 0
}
