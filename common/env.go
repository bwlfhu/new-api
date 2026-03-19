package common

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	PDEP_PROVIDER_SECRET        = "PDEP_PROVIDER_SECRET"
	PDEP_PROVIDER_OWNER_USER_ID = "PDEP_PROVIDER_OWNER_USER_ID"
)

func GetEnvOrDefault(env string, defaultValue int) int {
	if env == "" || os.Getenv(env) == "" {
		return defaultValue
	}
	num, err := strconv.Atoi(os.Getenv(env))
	if err != nil {
		SysError(fmt.Sprintf("failed to parse %s: %s, using default value: %d", env, err.Error(), defaultValue))
		return defaultValue
	}
	return num
}

func GetEnvOrDefaultString(env string, defaultValue string) string {
	if env == "" || os.Getenv(env) == "" {
		return defaultValue
	}
	return os.Getenv(env)
}

func GetEnvOrDefaultBool(env string, defaultValue bool) bool {
	if env == "" || os.Getenv(env) == "" {
		return defaultValue
	}
	b, err := strconv.ParseBool(os.Getenv(env))
	if err != nil {
		SysError(fmt.Sprintf("failed to parse %s: %s, using default value: %t", env, err.Error(), defaultValue))
		return defaultValue
	}
	return b
}

func GetPDEPProviderSecret() string {
	return strings.TrimSpace(GetEnvOrDefaultString(PDEP_PROVIDER_SECRET, ""))
}

func GetPDEPProviderOwnerUserID() int {
	return GetEnvOrDefault(PDEP_PROVIDER_OWNER_USER_ID, 0)
}
