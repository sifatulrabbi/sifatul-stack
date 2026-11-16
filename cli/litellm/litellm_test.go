package litellm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sifatulrabbi/sifatul-stack/cli/cost"
)

func excerpt(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/prices_excerpt.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseMapsCacheTiersAndSkipsNonTokenEntries(t *testing.T) {
	book, err := Parse(excerpt(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(book) != 4 {
		t.Fatalf("entries = %d, want 4 (sample_spec, empty and image-only entries skipped)", len(book))
	}
	opus, _ := book.Lookup("claude-opus-5-5")
	if opus != (cost.Price{Input: 4e-06, Output: 2e-05, CacheRead: 2e-07, CacheWrite5m: 5e-06, CacheWrite1h: 8e-06}) {
		t.Fatalf("opus = %+v", opus)
	}
	sol, _ := book.Lookup("gpt-6.1-sol")
	if sol.CacheWrite1h != sol.CacheWrite5m {
		t.Fatalf("missing 1h tier should fall back to 5m write price: %+v", sol)
	}
}

func TestMissingCachePricesFallBackToInputPrice(t *testing.T) {
	book, _ := Parse(excerpt(t))
	pro, ok := book.Lookup("gpt-5.5-pro")
	if !ok || pro.CacheRead != 3e-05 || pro.CacheWrite5m != 3e-05 {
		t.Fatalf("pro = %+v", pro)
	}
}

func TestLookupStripsContextSuffixAndTriesProviderPrefix(t *testing.T) {
	book, _ := Parse(excerpt(t))
	if _, ok := book.Lookup("claude-opus-5-5[1m]"); !ok {
		t.Error("[1m] suffix not stripped")
	}
	if _, ok := book.Lookup("only-prefixed"); !ok {
		t.Error("openai/ prefix not tried")
	}
	if _, ok := book.Lookup("<synthetic>"); ok {
		t.Error("unknown model priced")
	}
}

func TestStoreLoadWithoutFileSaysHowToInstall(t *testing.T) {
	_, err := Store{Path: filepath.Join(t.TempDir(), "nope.json")}.Load()
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("err = %v", err)
	}
}

func TestStoreUpdateDownloadsValidatesAndInstalls(t *testing.T) {
	raw := excerpt(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(raw) }))
	defer srv.Close()
	store := Store{Path: filepath.Join(t.TempDir(), "cache", "prices.json")}

	n, err := store.Update(context.Background(), srv.Client(), srv.URL)
	if err != nil || n != 4 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
	book, err := store.Load()
	if err != nil || len(book) != 4 {
		t.Fatalf("load: %d entries, err = %v", len(book), err)
	}
}

func TestStoreUpdateKeepsOldFileWhenDownloadIsGarbage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("<html>rate limited</html>")) }))
	defer srv.Close()
	store := Store{Path: filepath.Join(t.TempDir(), "prices.json")}
	os.WriteFile(store.Path, excerpt(t), 0o644)

	if _, err := store.Update(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("garbage download accepted")
	}
	if book, err := store.Load(); err != nil || len(book) != 4 {
		t.Fatalf("old prices lost: %d, %v", len(book), err)
	}
}
