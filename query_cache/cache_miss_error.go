package query_cache

import "github.com/turbot/go-kit/helpers"

type CacheMissError struct{}

func (CacheMissError) Error() string { return "cache miss" }

func IsCacheMiss(err error) bool {
	if err == nil {
		return false
	}
	// gocache returns "value not found in store"
	errorStrings := []string{CacheMissError{}.Error(), "value not found in store"}
	return helpers.StringSliceContains(errorStrings, err.Error())
}
