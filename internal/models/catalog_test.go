package models

import (
	"testing"

	"opencode2api/internal/config"
)

func TestRoutePreferAndAnonymousLanes(t *testing.T) {
	overrides := map[string]string{"free-model": "chat", "paid-model": "chat"}
	catalog := Catalog(config.TierGo, overrides)
	catalog.Replace([]string{"free-model", "paid-model"}, []string{"free-model", "paid-model"})

	route, err := catalog.Route("paid-model", true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if route.Anonymous || route.Tier != config.TierGo {
		t.Fatalf("prefer=go route: %#v", route)
	}
	if len(route.KeyTiers) == 0 || route.KeyTiers[0] != config.TierGo {
		t.Fatalf("key tier order: %#v", route.KeyTiers)
	}

	free, err := catalog.Route("free-model", false, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if !free.Anonymous || free.Tier != config.TierZen {
		t.Fatalf("anonymous route: %#v", free)
	}
}

func TestFreeOnlyHidesPaidModels(t *testing.T) {
	catalog := Catalog(config.TierZen, map[string]string{"free-model": "chat", "paid-model": "chat"})
	catalog.Replace([]string{"free-model", "paid-model"}, nil)
	catalog.SetFreeOnly(true)
	if _, err := catalog.Route("paid-model", true, false, false); err == nil {
		t.Fatal("paid model was routable with free_only enabled")
	}
	if _, err := catalog.Route("free-model", true, false, false); err != nil {
		t.Fatalf("free model was rejected: %v", err)
	}
	routes, _ := catalog.AvailableModels(true, false, false)
	if len(routes) != 1 || routes[0].ID != "free-model" {
		t.Fatalf("available models: %#v", routes)
	}
}
