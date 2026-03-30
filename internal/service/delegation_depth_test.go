package service

import (
	"context"
	"errors"
	"testing"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func rememberExpandTarget(t *testing.T, svc *AMMService) string {
	t.Helper()

	mem, err := svc.Remember(context.Background(), &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "delegation depth expand target",
		TightDescription: "delegation depth target",
	})
	if err != nil {
		t.Fatalf("remember target memory: %v", err)
	}
	return mem.ID
}

func TestExpand_DelegationDepthBlocked(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	svc.SetMaxExpandDepth(1)
	id := rememberExpandTarget(t, svc)

	_, err := svc.Expand(context.Background(), id, "memory", core.ExpandOptions{DelegationDepth: 2})
	if !errors.Is(err, core.ErrExpansionRecursionBlocked) {
		t.Fatalf("expected ErrExpansionRecursionBlocked, got %v", err)
	}
}

func TestExpand_DelegationDepthAllowed(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	svc.SetMaxExpandDepth(1)
	id := rememberExpandTarget(t, svc)

	result, err := svc.Expand(context.Background(), id, "memory", core.ExpandOptions{DelegationDepth: 0})
	if err != nil {
		t.Fatalf("expand with depth=0 should succeed: %v", err)
	}
	if result == nil || result.Memory == nil || result.Memory.ID != id {
		t.Fatalf("unexpected expand result: %+v", result)
	}
}

func TestExpand_DelegationDepthUnlimited(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	svc.SetMaxExpandDepth(-1)
	id := rememberExpandTarget(t, svc)

	result, err := svc.Expand(context.Background(), id, "memory", core.ExpandOptions{DelegationDepth: 99})
	if err != nil {
		t.Fatalf("expand with unlimited max depth should succeed: %v", err)
	}
	if result == nil || result.Memory == nil || result.Memory.ID != id {
		t.Fatalf("unexpected expand result: %+v", result)
	}
}

func TestExpand_DelegationDepthAbsentBackcompat(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	svc.SetMaxExpandDepth(1)
	id := rememberExpandTarget(t, svc)

	result, err := svc.Expand(context.Background(), id, "memory", core.ExpandOptions{})
	if err != nil {
		t.Fatalf("expand without delegation depth should remain compatible: %v", err)
	}
	if result == nil || result.Memory == nil || result.Memory.ID != id {
		t.Fatalf("unexpected expand result: %+v", result)
	}
}
