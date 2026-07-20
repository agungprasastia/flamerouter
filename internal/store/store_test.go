package store_test

import (
	"flamerouter/internal/store"
	"testing"
)

func setupTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestCombos(t *testing.T) {
	st := setupTestStore(t)

	id, err := st.CreateCombo("fast", []string{"openai/gpt-4o", "anthropic/claude-3-haiku"})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected id")
	}

	combos, err := st.ListCombos()
	if err != nil {
		t.Fatal(err)
	}
	if len(combos) != 1 {
		t.Fatalf("expected 1 combo, got %d", len(combos))
	}
	if combos[0].Name != "fast" {
		t.Fatalf("expected name 'fast', got %s", combos[0].Name)
	}
	if len(combos[0].Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(combos[0].Models))
	}

	combo, err := st.GetComboByName("fast")
	if err != nil {
		t.Fatal(err)
	}
	if combo == nil {
		t.Fatal("expected combo")
	}
	if combo.ID != id {
		t.Fatalf("expected id %s, got %s", id, combo.ID)
	}
}

func TestAliases(t *testing.T) {
	st := setupTestStore(t)

	err := st.SetAlias("gpt4", "openai/gpt-4o")
	if err != nil {
		t.Fatal(err)
	}

	aliases, err := st.ListAliases()
	if err != nil {
		t.Fatal(err)
	}
	if aliases["gpt4"] != "openai/gpt-4o" {
		t.Fatalf("expected openai/gpt-4o, got %s", aliases["gpt4"])
	}

	err = st.SetAlias("gpt4", "openai/gpt-4o-mini")
	if err != nil {
		t.Fatal(err)
	}
	aliases, _ = st.ListAliases()
	if aliases["gpt4"] != "openai/gpt-4o-mini" {
		t.Fatalf("expected openai/gpt-4o-mini after update, got %s", aliases["gpt4"])
	}
}

func TestProviderNodes(t *testing.T) {
	st := setupTestStore(t)

	id, err := st.CreateProviderNode("openai-compatible", "Custom OpenAI", "custom", "openai", "https://custom.api.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected id")
	}

	nodes, err := st.GetProviderNodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Prefix != "custom" {
		t.Fatalf("expected prefix 'custom', got %s", nodes[0].Prefix)
	}
}

func TestConnections(t *testing.T) {
	st := setupTestStore(t)

	id, err := st.CreateConnection("openai", "api_key", "test-key", "sk-test", "https://api.openai.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected id")
	}

	conns, err := st.ListActiveByProvider("openai")
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].APIKey != "sk-test" {
		t.Fatalf("expected api key sk-test, got %s", conns[0].APIKey)
	}
}

func TestUsage(t *testing.T) {
	st := setupTestStore(t)

	err := st.InsertUsage("openai", "gpt-4o", 100, 50, "conn1")
	if err != nil {
		t.Fatal(err)
	}
}

func TestComboNotFound(t *testing.T) {
	st := setupTestStore(t)

	combo, err := st.GetComboByName("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if combo != nil {
		t.Fatal("expected nil combo")
	}
}

func TestManagementRepos(t *testing.T) {
	st := setupTestStore(t)

	id, err := st.CreateProxyPool("pool1", "http", "127.0.0.1", 8080, "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateProxyPool(id, "pool1", "socks5", "127.0.0.1", 1080, "u", "p", true); err != nil {
		t.Fatal(err)
	}
	pools, err := st.ListProxyPools()
	if err != nil || len(pools) != 1 || pools[0].Type != "socks5" {
		t.Fatalf("pools: %+v err=%v", pools, err)
	}
	if err := st.DeleteProxyPool(id); err != nil {
		t.Fatal(err)
	}

	if err := st.DisableModel("openai/gpt-4o"); err != nil {
		t.Fatal(err)
	}
	dm, err := st.ListDisabledModels()
	if err != nil || len(dm) != 1 || dm[0] != "openai/gpt-4o" {
		t.Fatalf("disabled: %+v err=%v", dm, err)
	}
	if err := st.EnableModel("openai/gpt-4o"); err != nil {
		t.Fatal(err)
	}

	cmID, err := st.CreateCustomModel("openai", "my-model", "My Model", `{"vision":true}`)
	if err != nil {
		t.Fatal(err)
	}
	cms, err := st.ListCustomModels()
	if err != nil || len(cms) != 1 || cms[0].ID != cmID {
		t.Fatalf("custom: %+v err=%v", cms, err)
	}
	if err := st.DeleteCustomModel(cmID); err != nil {
		t.Fatal(err)
	}

	if err := st.InsertRequestDetail(store.RequestDetail{
		Provider: "openai", Model: "gpt-4o", StatusCode: 200, PromptTokens: 10, CompletionTokens: 5,
	}); err != nil {
		t.Fatal(err)
	}
	rds, err := st.QueryRequestDetails(10)
	if err != nil || len(rds) != 1 {
		t.Fatalf("request details: %+v err=%v", rds, err)
	}

	if err := st.InsertUsageDaily("2026-07-20", "openai", "gpt-4o", 1, 10, 5); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertUsageDaily("2026-07-20", "openai", "gpt-4o", 2, 20, 10); err != nil {
		t.Fatal(err)
	}
	ud, err := st.QueryUsageDaily("2026-07-01", "2026-07-31")
	if err != nil || len(ud) != 1 || ud[0].Requests != 3 {
		t.Fatalf("usage daily: %+v err=%v", ud, err)
	}
	chart, err := st.QueryUsageChart("2026-07-01", "2026-07-31")
	if err != nil || len(chart) != 1 || chart[0].Requests != 3 {
		t.Fatalf("usage chart: %+v err=%v", chart, err)
	}

	if err := st.KVSet("cli-tools", "foo", "bar"); err != nil {
		t.Fatal(err)
	}
	v, err := st.KVGet("cli-tools", "foo")
	if err != nil || v != "bar" {
		t.Fatalf("kv get: %q err=%v", v, err)
	}
	if err := st.KVDelete("cli-tools", "foo"); err != nil {
		t.Fatal(err)
	}
	v, _ = st.KVGet("cli-tools", "foo")
	if v != "" {
		t.Fatalf("expected empty after delete, got %q", v)
	}

	// connection strategy columns present after migration
	connID, err := st.CreateConnection("openai", "api_key", "c1", "sk", "")
	if err != nil {
		t.Fatal(err)
	}
	conns, err := st.ListActiveByProvider("openai")
	if err != nil || len(conns) != 1 || conns[0].ID != connID {
		t.Fatalf("conns: %+v err=%v", conns, err)
	}
}
