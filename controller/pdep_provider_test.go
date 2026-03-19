package controller_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/router"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

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

	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("failed to migrate user table: %v", err)
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

func newPDEPProviderTestRouter() *gin.Engine {
	engine := gin.New()
	router.SetApiRouter(engine)
	return engine
}

func TestPDEPProviderAuth_RejectsMissingBearer(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 1101, common.UserStatusEnabled)
	t.Setenv("PDEP_PROVIDER_SECRET", "test-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "1101")

	router := newPDEPProviderTestRouter()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/pdep/v1/tokens", nil)

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing Authorization, got %d", recorder.Code)
	}
}

func TestPDEPProviderAuth_RejectsWrongSecret(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 1201, common.UserStatusEnabled)
	t.Setenv("PDEP_PROVIDER_SECRET", "expected-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "1201")

	router := newPDEPProviderTestRouter()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/pdep/v1/tokens", nil)
	req.Header.Set("Authorization", "Bearer wrong-secret")

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong secret, got %d", recorder.Code)
	}
}

func TestPDEPProviderTokens_RejectsMissingOwnerConfig(t *testing.T) {
	t.Setenv("PDEP_PROVIDER_SECRET", "expected-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", "")

	router := newPDEPProviderTestRouter()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/pdep/v1/tokens", nil)
	req.Header.Set("Authorization", "Bearer expected-secret")

	router.ServeHTTP(recorder, req)

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

			router := newPDEPProviderTestRouter()
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/pdep/v1/tokens", nil)
			req.Header.Set("Authorization", "Bearer expected-secret")

			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("expected 403 for invalid owner user, got %d", recorder.Code)
			}
		})
	}
}

func TestPDEPProviderRoutes_ValidAuth_ReachesPlaceholderController(t *testing.T) {
	db := setupPDEPProviderControllerTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 2101, common.UserStatusEnabled)
	t.Setenv("PDEP_PROVIDER_SECRET", "valid-secret")
	t.Setenv("PDEP_PROVIDER_OWNER_USER_ID", " 2101 ")

	testRoutes := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "get-tokens",
			method: http.MethodGet,
			path:   "/api/pdep/v1/tokens",
		},
		{
			name:   "post-tokens",
			method: http.MethodPost,
			path:   "/api/pdep/v1/tokens",
		},
		{
			name:   "delete-token",
			method: http.MethodDelete,
			path:   "/api/pdep/v1/tokens/1",
		},
		{
			name:   "get-aggregated-tokens",
			method: http.MethodGet,
			path:   "/api/pdep/v1/tokens/aggregated",
		},
	}

	for _, testRoute := range testRoutes {
		t.Run(testRoute.name, func(t *testing.T) {
			router := newPDEPProviderTestRouter()
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(testRoute.method, testRoute.path, nil)
			req.Header.Set("Authorization", "Bearer valid-secret")

			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNotImplemented {
				t.Fatalf("expected 501 for %s %s, got %d", testRoute.method, testRoute.path, recorder.Code)
			}
		})
	}
}
