package controller_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	if err := db.AutoMigrate(&model.User{}, &model.Token{}, &model.Log{}); err != nil {
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
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "2101")

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
			statusCode: http.StatusNotImplemented,
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
