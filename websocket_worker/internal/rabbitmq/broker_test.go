package rabbitmq

import "testing"

func TestDeliveryEffectiveTargetsSupportsBatchAndLegacyPayloads(t *testing.T) {
	batched := Delivery{
		Targets: []DeliveryTarget{
			{UserID: "user-a", ConnectionIDs: []string{"a-1", "a-2"}},
			{UserID: "user-b", ConnectionIDs: []string{"b-1"}},
		},
	}
	if got := batched.EffectiveTargets(); len(got) != 2 {
		t.Fatalf("expected 2 batched targets, got %d", len(got))
	}

	legacy := Delivery{UserID: "user-a", ConnectionIDs: []string{"a-1"}}
	got := legacy.EffectiveTargets()
	if len(got) != 1 || got[0].UserID != "user-a" || len(got[0].ConnectionIDs) != 1 {
		t.Fatalf("unexpected legacy target conversion: %#v", got)
	}

	if got := (Delivery{}).EffectiveTargets(); got != nil {
		t.Fatalf("expected empty delivery to have no effective targets, got %#v", got)
	}
}
