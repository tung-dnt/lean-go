package pager

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func request(target string) *http.Request {
	return httptest.NewRequest(http.MethodGet, target, nil)
}

func TestNewPager(t *testing.T) {
	pgr := NewPager(request("/"), 10)
	assert.Equal(t, 10, pgr.ItemsPerPage)
	assert.Equal(t, 1, pgr.Page)
	assert.Equal(t, 0, pgr.Items)
	assert.Equal(t, 1, pgr.Pages)

	pgr = NewPager(request(fmt.Sprintf("/abc?%s=%d", QueryKey, 2)), 10)
	assert.Equal(t, 2, pgr.Page)

	pgr = NewPager(request(fmt.Sprintf("/abc?%s=%d", QueryKey, -2)), 10)
	assert.Equal(t, 1, pgr.Page, "a page before the first is the first")

	pgr = NewPager(request("/abc?page=two"), 10)
	assert.Equal(t, 1, pgr.Page, "and so is one that is not a number")
}

func TestPager_SetItems(t *testing.T) {
	pgr := NewPager(request("/"), 20)
	pgr.SetItems(100)
	assert.Equal(t, 100, pgr.Items)
	assert.Equal(t, 5, pgr.Pages)

	pgr.SetItems(0)
	assert.Equal(t, 0, pgr.Items)
	assert.Equal(t, 1, pgr.Pages)
	assert.Equal(t, 1, pgr.Page)

	pgr = NewPager(request("/?page=9"), 20)
	pgr.SetItems(41)
	assert.Equal(t, 3, pgr.Page, "a page past the last is the last")
}

func TestPager_IsBeginning(t *testing.T) {
	pgr := NewPager(request("/"), 20)
	pgr.Pages = 10
	assert.True(t, pgr.IsBeginning())
	pgr.Page = 2
	assert.False(t, pgr.IsBeginning())
	pgr.Page = 1
	assert.True(t, pgr.IsBeginning())
}

func TestPager_IsEnd(t *testing.T) {
	pgr := NewPager(request("/"), 20)
	pgr.Pages = 10
	assert.False(t, pgr.IsEnd())
	pgr.Page = 10
	assert.True(t, pgr.IsEnd())
	pgr.Page = 1
	assert.False(t, pgr.IsEnd())
}

func TestPager_GetOffset(t *testing.T) {
	pgr := NewPager(request("/"), 20)
	assert.Equal(t, 0, pgr.GetOffset())
	pgr.Page = 2
	assert.Equal(t, 20, pgr.GetOffset())
	pgr.Page = 3
	assert.Equal(t, 40, pgr.GetOffset())
}
