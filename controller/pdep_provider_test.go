package controller_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/router"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type pdepListResponse struct {
	Items []struct {
		ID        string  `json:"id"`
		Name      string  `json:"name"`
		DisplayID string  `json:"displayId"`
		KeyPrefix string  `json:"keyPrefix"`
		LastUsed  *string `json:"lastUsed,omitempty"`
		CreatedAt string  `json:"createdAt"`
		IsActive  bool    `json:"isActive"`
	} `json:"items"`
}

type pdepCreateResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	DisplayID string `json:"displayId"`
	KeyPrefix string `json:"keyPrefix"`
	CreatedAt string `json:"createdAt"`
	IsActive  bool   `json:"isActive"`
	Key       string `json:"key"`
}

type pdepErrorResponse struct {
	Message string `json:"message"`
}

type pdepAggregatedResponse struct {
	Buckets []struct {
		Timestamp string `json:"timestamp"`
		Usage        int64 `json:"usage"`
		Refill       int64 `json:"refill"`
		Net          int64 `json:"net"`
		TokenUsed    int64 `json:"tokenUsed"`
		QuotaUsed    int64 `json:"quotaUsed"`
		RequestCount int64 `json:"requestCount"`
	} `json:"buckets"`
}

func setupPDEPProviderControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.GlobalApiRateLimitEnable = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	model.DB = db
	model.LOG_DB = db

	if err := db.AutoMigrate(&model.User{}, &model.Token{}, &model.Log{}, &model.PDEPTokenUsageBucket{}); err != nil {
		t.Fatalf("failed to migrate tables: %v", err)
	}

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func seedPDEPProviderOwnerUser(t *testing.T, db *gorm.DB, id int, status int) {
	t.Helper()

	user := &model.User{
		Id:          id,
		Username:    fmt.Sprintf("owner_%d", id),
		Password:    "password123",
		Role:        common.RoleAdminUser,
		Status:      status,
		DisplayName: fmt.Sprintf("Owner %d", id),
		Email:       fmt.Sprintf("owner_%d@example.com", id),
		Group:       "default",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create owner user: %v", err)
	}
}

func seedPDEPProviderToken(t *testing.T, db *gorm.DB, userID int, name string, key string, createdTime int64) *model.Token {
	t.Helper()

	token := &model.Token{
		UserId:       userID,
		Name:         name,
		Key:          key,
		Status:       common.TokenStatusEnabled,
		CreatedTime:  createdTime,
		AccessedTime: createdTime,
		ExpiredTime:  -1,
	}
	if err := db.Create(token).Error; err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
	return token
}

func newPDEPProviderTestRouter() *gin.Engine {
	engine := gin.New()
	router.SetApiRouter(engine)
	return engine
}

func doPDEPProviderRequest(t *testing.T, engine *gin.Engine, method string, path string, body []byte, secret string) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}

	engine.ServeHTTP(recorder, req)
	return recorder
}

func TestPDEPProviderAuth_RejectsMissingBearer(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 1101, common.UserStatusEnabled)
	t.Setenv("PDEP_PROVIDER_SECRET", "test-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "1101")

	engine := newPDEPProviderTestRouter()
	recorder := doPDEPProviderRequest(t, engine, http.MethodGet, "/api/pdep/v1/tokens", nil, "")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing Authorization, got %d", recorder.Code)
	}
}

func TestPDEPProviderAuth_RejectsWrongSecret(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 1201, common.UserStatusEnabled)
	t.Setenv("PDEP_PROVIDER_SECRET", "expected-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "1201")

	engine := newPDEPProviderTestRouter()
	recorder := doPDEPProviderRequest(t, engine, http.MethodGet, "/api/pdep/v1/tokens", nil, "wrong-secret")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong secret, got %d", recorder.Code)
	}
}

func TestPDEPProviderTokens_RejectsMissingOwnerConfig(t *testing.T) {
	t.Setenv("PDEP_PROVIDER_SECRET", "expected-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "")

	engine := newPDEPProviderTestRouter()
	recorder := doPDEPProviderRequest(t, engine, http.MethodGet, "/api/pdep/v1/tokens", nil, "expected-secret")

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when owner config is missing, got %d", recorder.Code)
	}
}

func TestPDEPProviderTokens_RejectsInvalidOwnerUser(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	t.Setenv("PDEP_PROVIDER_SECRET", "expected-secret")

	testCases := []struct {
		name   string
		id     int
		seed   bool
		status int
	}{
		{
			name:   "owner-not-found",
			id:     1301,
			seed:   false,
			status: common.UserStatusEnabled,
		},
		{
			name:   "owner-disabled",
			id:     1302,
			seed:   true,
			status: common.UserStatusDisabled,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.seed {
				seedPDEPProviderOwnerUser(t, db, testCase.id, testCase.status)
			}
			t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", fmt.Sprintf("%d", testCase.id))

			engine := newPDEPProviderTestRouter()
			recorder := doPDEPProviderRequest(t, engine, http.MethodGet, "/api/pdep/v1/tokens", nil, "expected-secret")

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("expected 403 for invalid owner user, got %d", recorder.Code)
			}
		})
	}
}

func TestPDEPProviderRoutes_ValidAuth_ReachesController(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 2101, common.UserStatusEnabled)
	t.Setenv("PDEP_PROVIDER_SECRET", "valid-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", " 2101 ")

	testRoutes := []struct {
		name       string
		method     string
		path       string
		body       []byte
		statusCode int
	}{
		{
			name:       "get-tokens",
			method:     http.MethodGet,
			path:       "/api/pdep/v1/tokens",
			statusCode: http.StatusOK,
		},
		{
			name:       "post-tokens",
			method:     http.MethodPost,
			path:       "/api/pdep/v1/tokens",
			body:       []byte(`{"name":"route-create"}`),
			statusCode: http.StatusOK,
		},
		{
			name:       "delete-token",
			method:     http.MethodDelete,
			path:       "/api/pdep/v1/tokens/1",
			statusCode: http.StatusOK,
		},
		{
			name:       "get-aggregated-tokens",
			method:     http.MethodGet,
			path:       "/api/pdep/v1/tokens/aggregated",
			statusCode: http.StatusBadRequest,
		},
	}

	for _, testRoute := range testRoutes {
		t.Run(testRoute.name, func(t *testing.T) {
			engine := newPDEPProviderTestRouter()
			recorder := doPDEPProviderRequest(t, engine, testRoute.method, testRoute.path, testRoute.body, "valid-secret")
			if recorder.Code != testRoute.statusCode {
				t.Fatalf("expected %d for %s %s, got %d", testRoute.statusCode, testRoute.method, testRoute.path, recorder.Code)
			}
		})
	}
}

func TestPDEPListTokens_ReturnsDisplayIDAndKeyPrefix(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 3101, common.UserStatusEnabled)
	token := seedPDEPProviderToken(t, db, 3101, "huyuwen", "abcd1234owner31010000000000000000000000000000", 1710752400)
	seedPDEPProviderToken(t, db, 3102, "other", "wxyz1234owner31020000000000000000000000000000", 1710752400)
	t.Setenv("PDEP_PROVIDER_SECRET", "test-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "3101")

	engine := newPDEPProviderTestRouter()
	recorder := doPDEPProviderRequest(t, engine, http.MethodGet, "/api/pdep/v1/tokens", nil, "test-secret")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	var response pdepListResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	if len(response.Items) != 1 {
		t.Fatalf("expected only owner token, got %d", len(response.Items))
	}

	item := response.Items[0]
	if item.ID != fmt.Sprintf("%d", token.Id) {
		t.Fatalf("expected id %d, got %s", token.Id, item.ID)
	}
	if item.DisplayID != fmt.Sprintf("token-%d", token.Id) {
		t.Fatalf("expected displayId token-%d, got %s", token.Id, item.DisplayID)
	}
	if item.KeyPrefix != "sk-abcd" {
		t.Fatalf("expected keyPrefix sk-abcd, got %s", item.KeyPrefix)
	}
	if item.CreatedAt != time.Unix(token.CreatedTime, 0).UTC().Format(time.RFC3339) {
		t.Fatalf("expected createdAt %s, got %s", time.Unix(token.CreatedTime, 0).UTC().Format(time.RFC3339), item.CreatedAt)
	}
}

func TestPDEPListTokens_ReturnsLastUsed(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 3201, common.UserStatusEnabled)
	token := seedPDEPProviderToken(t, db, 3201, "huyuwen", "abcd1234owner32010000000000000000000000000000", 1710752400)
	t.Setenv("PDEP_PROVIDER_SECRET", "test-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "3201")

	if err := db.Create(&model.Log{
		UserId:    3201,
		TokenId:   token.Id,
		Type:      model.LogTypeConsume,
		CreatedAt: 1710820800,
		Content:   "consume-old",
	}).Error; err != nil {
		t.Fatalf("failed to seed consume log: %v", err)
	}
	if err := db.Create(&model.Log{
		UserId:    3201,
		TokenId:   token.Id,
		Type:      model.LogTypeManage,
		CreatedAt: 1710907200,
		Content:   "manage-newer",
	}).Error; err != nil {
		t.Fatalf("failed to seed manage log: %v", err)
	}
	if err := db.Create(&model.Log{
		UserId:    3201,
		TokenId:   token.Id,
		Type:      model.LogTypeConsume,
		CreatedAt: 1710993600,
		Content:   "consume-newest",
	}).Error; err != nil {
		t.Fatalf("failed to seed consume log 2: %v", err)
	}

	engine := newPDEPProviderTestRouter()
	recorder := doPDEPProviderRequest(t, engine, http.MethodGet, "/api/pdep/v1/tokens", nil, "test-secret")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	var response pdepListResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	if len(response.Items) != 1 {
		t.Fatalf("expected only one item, got %d", len(response.Items))
	}
	if response.Items[0].LastUsed == nil {
		t.Fatalf("expected lastUsed present")
	}
	expected := time.Unix(1710993600, 0).UTC().Format(time.RFC3339)
	if *response.Items[0].LastUsed != expected {
		t.Fatalf("expected lastUsed %s, got %s", expected, *response.Items[0].LastUsed)
	}
}

func TestPDEPCreateToken_ReturnsKeyOnlyOnce(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 3301, common.UserStatusEnabled)
	t.Setenv("PDEP_PROVIDER_SECRET", "test-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "3301")

	engine := newPDEPProviderTestRouter()
	createRecorder := doPDEPProviderRequest(t, engine, http.MethodPost, "/api/pdep/v1/tokens", []byte(`{"name":"huyuwen"}`), "test-secret")
	if createRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200 on create, got %d", createRecorder.Code)
	}

	var createResp pdepCreateResponse
	if err := common.Unmarshal(createRecorder.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}
	if !strings.HasPrefix(createResp.Key, "sk-") {
		t.Fatalf("expected key starts with sk-, got %s", createResp.Key)
	}
	if createResp.KeyPrefix == "" || !strings.HasPrefix(createResp.KeyPrefix, "sk-") {
		t.Fatalf("expected keyPrefix starts with sk-, got %s", createResp.KeyPrefix)
	}

	listRecorder := doPDEPProviderRequest(t, engine, http.MethodGet, "/api/pdep/v1/tokens", nil, "test-secret")
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200 on list, got %d", listRecorder.Code)
	}

	var listMap map[string]any
	if err := common.Unmarshal(listRecorder.Body.Bytes(), &listMap); err != nil {
		t.Fatalf("failed to decode list response map: %v", err)
	}
	rawItems, ok := listMap["items"].([]any)
	if !ok || len(rawItems) != 1 {
		t.Fatalf("expected one item in list response, got %#v", listMap["items"])
	}
	item, ok := rawItems[0].(map[string]any)
	if !ok {
		t.Fatalf("expected item object, got %#v", rawItems[0])
	}
	if _, hasKey := item["key"]; hasKey {
		t.Fatalf("expected list item does not contain key field")
	}
}

func TestPDEPDeleteToken_ReturnsSuccessWhenMissing(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 3401, common.UserStatusEnabled)
	t.Setenv("PDEP_PROVIDER_SECRET", "test-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "3401")

	engine := newPDEPProviderTestRouter()
	recorder := doPDEPProviderRequest(t, engine, http.MethodDelete, "/api/pdep/v1/tokens/999999", nil, "test-secret")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for missing token delete, got %d", recorder.Code)
	}
}

func TestPDEPDeleteToken_RejectsNonOwnerToken(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 3501, common.UserStatusEnabled)
	token := seedPDEPProviderToken(t, db, 3502, "other-owner", "abcd1234owner35020000000000000000000000000000", 1710752400)
	t.Setenv("PDEP_PROVIDER_SECRET", "test-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "3501")

	engine := newPDEPProviderTestRouter()
	recorder := doPDEPProviderRequest(t, engine, http.MethodDelete, fmt.Sprintf("/api/pdep/v1/tokens/%d", token.Id), nil, "test-secret")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for forbidden delete, got %d", recorder.Code)
	}

	var response pdepErrorResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if response.Message != "forbidden" {
		t.Fatalf("expected message forbidden, got %s", response.Message)
	}
}

func TestPDEPGetTokenAggregated_RejectsInvalidTimeRange(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 6101, common.UserStatusEnabled)
	token := seedPDEPProviderToken(t, db, 6101, "owner", "abcd1234owner61010000000000000000000000000000", 1710752400)
	t.Setenv("PDEP_PROVIDER_SECRET", "test-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "6101")

	engine := newPDEPProviderTestRouter()
	start := "2026-03-19T01:00:00Z"
	end := "2026-03-19T01:00:00Z"
	path := fmt.Sprintf("/api/pdep/v1/tokens/aggregated?sourceId=token-%d&startTime=%s&endTime=%s", token.Id, url.QueryEscape(start), url.QueryEscape(end))
	recorder := doPDEPProviderRequest(t, engine, http.MethodGet, path, nil, "test-secret")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid time range, got %d", recorder.Code)
	}

	var response pdepErrorResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if response.Message == "" {
		t.Fatalf("expected non-empty error message")
	}
}

func TestPDEPGetTokenAggregated_RejectsUnalignedTimeRange(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 6151, common.UserStatusEnabled)
	token := seedPDEPProviderToken(t, db, 6151, "owner", "abcd1234owner61510000000000000000000000000000", 1710752400)
	t.Setenv("PDEP_PROVIDER_SECRET", "test-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "6151")

	engine := newPDEPProviderTestRouter()
	start := "2026-03-19T00:05:00Z"
	end := "2026-03-19T00:15:00Z"
	path := fmt.Sprintf("/api/pdep/v1/tokens/aggregated?sourceId=token-%d&startTime=%s&endTime=%s", token.Id, url.QueryEscape(start), url.QueryEscape(end))
	recorder := doPDEPProviderRequest(t, engine, http.MethodGet, path, nil, "test-secret")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unaligned time range, got %d", recorder.Code)
	}

	var response pdepErrorResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if response.Message != "invalid time range" {
		t.Fatalf("expected message invalid time range, got %s", response.Message)
	}
}

func TestPDEPGetTokenAggregated_AcceptsUTCPlusZeroOffset(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 6171, common.UserStatusEnabled)
	token := seedPDEPProviderToken(t, db, 6171, "owner", "abcd1234owner61710000000000000000000000000000", 1710752400)
	t.Setenv("PDEP_PROVIDER_SECRET", "test-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "6171")

	engine := newPDEPProviderTestRouter()
	start := "2026-03-19T00:00:00+00:00"
	end := "2026-03-19T00:10:00+00:00"
	path := fmt.Sprintf("/api/pdep/v1/tokens/aggregated?sourceId=token-%d&startTime=%s&endTime=%s", token.Id, url.QueryEscape(start), url.QueryEscape(end))
	recorder := doPDEPProviderRequest(t, engine, http.MethodGet, path, nil, "test-secret")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for utc +00:00, got %d", recorder.Code)
	}
}

func TestPDEPGetTokenAggregated_RejectsNonUTCOffset(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 6181, common.UserStatusEnabled)
	token := seedPDEPProviderToken(t, db, 6181, "owner", "abcd1234owner61810000000000000000000000000000", 1710752400)
	t.Setenv("PDEP_PROVIDER_SECRET", "test-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "6181")

	engine := newPDEPProviderTestRouter()
	start := "2026-03-19T08:00:00+08:00"
	end := "2026-03-19T08:10:00+08:00"
	path := fmt.Sprintf("/api/pdep/v1/tokens/aggregated?sourceId=token-%d&startTime=%s&endTime=%s", token.Id, url.QueryEscape(start), url.QueryEscape(end))
	recorder := doPDEPProviderRequest(t, engine, http.MethodGet, path, nil, "test-secret")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-utc offset, got %d", recorder.Code)
	}

	var response pdepErrorResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if response.Message != "invalid time range" {
		t.Fatalf("expected message invalid time range, got %s", response.Message)
	}
}

func TestPDEPGetTokenAggregated_RejectsNonOwnerTokenWith403(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 6201, common.UserStatusEnabled)
	token := seedPDEPProviderToken(t, db, 6202, "other-owner", "abcd1234owner62020000000000000000000000000000", 1710752400)
	t.Setenv("PDEP_PROVIDER_SECRET", "test-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "6201")

	engine := newPDEPProviderTestRouter()
	start := "2026-03-19T00:00:00Z"
	end := "2026-03-19T01:00:00Z"
	path := fmt.Sprintf("/api/pdep/v1/tokens/aggregated?sourceId=token-%d&startTime=%s&endTime=%s", token.Id, url.QueryEscape(start), url.QueryEscape(end))
	recorder := doPDEPProviderRequest(t, engine, http.MethodGet, path, nil, "test-secret")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-owner token, got %d", recorder.Code)
	}

	var response pdepErrorResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode forbidden response: %v", err)
	}
	if response.Message != "forbidden" {
		t.Fatalf("expected message forbidden, got %s", response.Message)
	}
}

func TestPDEPGetTokenAggregated_ReturnsTenMinuteBuckets(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 6301, common.UserStatusEnabled)
	token := seedPDEPProviderToken(t, db, 6301, "owner", "abcd1234owner63010000000000000000000000000000", 1710752400)
	t.Setenv("PDEP_PROVIDER_SECRET", "test-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "6301")

	start := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 19, 0, 30, 0, 0, time.UTC)
	seedTs := start.Unix()
	// 插入乱序数据，验证接口返回按 bucket_start asc 排序。
	if err := db.Create(&model.PDEPTokenUsageBucket{
		OwnerID:      6301,
		TokenID:      token.Id,
		BucketStart:  start.Add(10 * time.Minute).Unix(),
		TokenUsed:    190,
		QuotaUsed:    80,
		RequestCount: 1,
		CreatedAt:    seedTs,
		UpdatedAt:    seedTs,
	}).Error; err != nil {
		t.Fatalf("failed to seed usage bucket 2 (out-of-order insert): %v", err)
	}
	if err := db.Create(&model.PDEPTokenUsageBucket{
		OwnerID:      6301,
		TokenID:      token.Id,
		BucketStart:  start.Unix(),
		TokenUsed:    250,
		QuotaUsed:    100,
		RequestCount: 2,
		CreatedAt:    seedTs,
		UpdatedAt:    seedTs,
	}).Error; err != nil {
		t.Fatalf("failed to seed usage bucket 1: %v", err)
	}
	// 边界桶：bucket_start < start 以及 bucket_start == end 必须被排除（查询范围 [start, end)）。
	if err := db.Create(&model.PDEPTokenUsageBucket{
		OwnerID:      6301,
		TokenID:      token.Id,
		BucketStart:  start.Add(-1 * time.Second).Unix(),
		TokenUsed:    9999,
		QuotaUsed:    9999,
		RequestCount: 9999,
		CreatedAt:    seedTs,
		UpdatedAt:    seedTs,
	}).Error; err != nil {
		t.Fatalf("failed to seed usage bucket start-1s: %v", err)
	}
	if err := db.Create(&model.PDEPTokenUsageBucket{
		OwnerID:      6301,
		TokenID:      token.Id,
		BucketStart:  end.Unix(),
		TokenUsed:    8888,
		QuotaUsed:    8888,
		RequestCount: 8888,
		CreatedAt:    seedTs,
		UpdatedAt:    seedTs,
	}).Error; err != nil {
		t.Fatalf("failed to seed usage bucket end: %v", err)
	}
	// 种入与桶表矛盾的 logs，证明接口聚合不回退扫 logs。
	if err := db.Create(&model.Log{
		UserId:    6301,
		TokenId:   token.Id,
		Type:      model.LogTypeConsume,
		Quota:     777777,
		CreatedAt: start.Add(60 * time.Second).Unix(),
	}).Error; err != nil {
		t.Fatalf("failed to seed conflicting consume log: %v", err)
	}

	engine := newPDEPProviderTestRouter()
	path := fmt.Sprintf(
		"/api/pdep/v1/tokens/aggregated?sourceId=token-%d&startTime=%s&endTime=%s",
		token.Id,
		url.QueryEscape(start.Format(time.RFC3339)),
		url.QueryEscape(end.Format(time.RFC3339)),
	)
	recorder := doPDEPProviderRequest(t, engine, http.MethodGet, path, nil, "test-secret")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	var response pdepAggregatedResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode aggregated response: %v", err)
	}
	if len(response.Buckets) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(response.Buckets))
	}
	if response.Buckets[0].Timestamp != "2026-03-19T00:00:00Z" ||
		response.Buckets[0].Usage != 100 ||
		response.Buckets[0].Refill != 0 ||
		response.Buckets[0].Net != 100 ||
		response.Buckets[0].TokenUsed != 250 ||
		response.Buckets[0].QuotaUsed != 100 ||
		response.Buckets[0].RequestCount != 2 {
		t.Fatalf("unexpected first bucket: %+v", response.Buckets[0])
	}
	if response.Buckets[1].Timestamp != "2026-03-19T00:10:00Z" ||
		response.Buckets[1].Usage != 80 ||
		response.Buckets[1].Refill != 0 ||
		response.Buckets[1].Net != 80 ||
		response.Buckets[1].TokenUsed != 190 ||
		response.Buckets[1].QuotaUsed != 80 ||
		response.Buckets[1].RequestCount != 1 {
		t.Fatalf("unexpected second bucket: %+v", response.Buckets[1])
	}
}

func TestPDEPGetTokenAggregated_FallsBackToLogsWhenBucketMissing(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 6401, common.UserStatusEnabled)
	token := seedPDEPProviderToken(t, db, 6401, "owner", "abcd1234owner64010000000000000000000000000000", 1710752400)
	t.Setenv("PDEP_PROVIDER_SECRET", "test-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "6401")

	start := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 19, 0, 30, 0, 0, time.UTC)
	seedTs := start.Unix()

	// 桶表只种第一桶（00:00）。
	if err := db.Create(&model.PDEPTokenUsageBucket{
		OwnerID:      6401,
		TokenID:      token.Id,
		BucketStart:  start.Unix(),
		TokenUsed:    250,
		QuotaUsed:    100,
		RequestCount: 2,
		CreatedAt:    seedTs,
		UpdatedAt:    seedTs,
	}).Error; err != nil {
		t.Fatalf("failed to seed usage bucket 1: %v", err)
	}

	// logs 种第二桶（00:10），作为缺桶兜底来源。
	if err := db.Create(&model.Log{
		UserId:           6401,
		TokenId:          token.Id,
		Type:             model.LogTypeConsume,
		Quota:            33,
		PromptTokens:     7,
		CompletionTokens: 11,
		CreatedAt:        start.Add(11 * time.Minute).Unix(),
	}).Error; err != nil {
		t.Fatalf("failed to seed consume log for fallback: %v", err)
	}

	engine := newPDEPProviderTestRouter()
	path := fmt.Sprintf(
		"/api/pdep/v1/tokens/aggregated?sourceId=token-%d&startTime=%s&endTime=%s",
		token.Id,
		url.QueryEscape(start.Format(time.RFC3339)),
		url.QueryEscape(end.Format(time.RFC3339)),
	)
	recorder := doPDEPProviderRequest(t, engine, http.MethodGet, path, nil, "test-secret")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	var response pdepAggregatedResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode aggregated response: %v", err)
	}
	if len(response.Buckets) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(response.Buckets))
	}

	// 第一桶来自桶表。
	if response.Buckets[0].Timestamp != "2026-03-19T00:00:00Z" ||
		response.Buckets[0].Usage != 100 ||
		response.Buckets[0].Refill != 0 ||
		response.Buckets[0].Net != 100 ||
		response.Buckets[0].TokenUsed != 250 ||
		response.Buckets[0].QuotaUsed != 100 ||
		response.Buckets[0].RequestCount != 2 {
		t.Fatalf("unexpected first bucket: %+v", response.Buckets[0])
	}

	// 第二桶来自 logs 聚合：token_used=7+11=18, quota_used=33, request_count=1。
	if response.Buckets[1].Timestamp != "2026-03-19T00:10:00Z" ||
		response.Buckets[1].Usage != 33 ||
		response.Buckets[1].Refill != 0 ||
		response.Buckets[1].Net != 33 ||
		response.Buckets[1].TokenUsed != 18 ||
		response.Buckets[1].QuotaUsed != 33 ||
		response.Buckets[1].RequestCount != 1 {
		t.Fatalf("unexpected second bucket: %+v", response.Buckets[1])
	}
}
