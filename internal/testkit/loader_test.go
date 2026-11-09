package testkit

import (
	"context"
	"testing"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/provider"
)

// providerLoader 是测试内部用的索引加载小工具。
type providerLoader struct{ baseURL string }

func (p *providerLoader) load(t *testing.T, prov *provider.Provider) (*provider.Index, error) {
	t.Helper()
	loader := &provider.IndexLoader{Client: fetch.New(), CacheDir: t.TempDir()}
	return loader.Load(context.Background(), prov, "default")
}
