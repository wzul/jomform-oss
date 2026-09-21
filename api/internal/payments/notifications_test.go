package payments

import (
	"context"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestNotificationConditionMet covers the conditional-logic ops.
func TestNotificationConditionMet(t *testing.T) {
	fields := map[string]string{"tier": "gold", "qty": "5", "note": "hello world"}
	cases := []struct {
		name string
		cond *dbstore.NotificationCondition
		want bool
	}{
		{"nil always matches", nil, true},
		{"equals match", &dbstore.NotificationCondition{FieldID: "tier", Op: "equals", Value: "gold"}, true},
		{"equals no match", &dbstore.NotificationCondition{FieldID: "tier", Op: "equals", Value: "silver"}, false},
		{"equals case-insensitive", &dbstore.NotificationCondition{FieldID: "tier", Op: "equals", Value: "GOLD"}, true},
		{"not_equals match", &dbstore.NotificationCondition{FieldID: "tier", Op: "not_equals", Value: "silver"}, true},
		{"contains match", &dbstore.NotificationCondition{FieldID: "note", Op: "contains", Value: "HELLO"}, true},
		{"gt match", &dbstore.NotificationCondition{FieldID: "qty", Op: "gt", Value: "3"}, true},
		{"gt no match", &dbstore.NotificationCondition{FieldID: "qty", Op: "gt", Value: "10"}, false},
		{"lt match", &dbstore.NotificationCondition{FieldID: "qty", Op: "lt", Value: "10"}, true},
		{"not_empty match", &dbstore.NotificationCondition{FieldID: "tier", Op: "not_empty", Value: ""}, true},
		{"not_empty no match", &dbstore.NotificationCondition{FieldID: "missing", Op: "not_empty", Value: ""}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := notificationConditionMet(c.cond, fields); got != c.want {
				t.Fatalf("conditionMet(%+v) = %v, want %v", c.cond, got, c.want)
			}
		})
	}
}

// TestRenderNotification verifies placeholders + field values render.
func TestRenderNotification(t *testing.T) {
	ref := NotificationRef{
		RefCode: "ABC123", AmountSen: 1500, ItemsSummary: "Widget x1",
		BuyerName: "Ali", FormURL: "https://shop.jomform.com/w",
		Fields: map[string]string{"email": "buyer@example.com", "tier": "gold"},
	}
	subj, body := renderNotification("Order {{ref_code}} for {{tier}}", "Hi {{buyer_name}}, total RM {{amount}} to {{email}}", ref)
	if subj != "Order ABC123 for gold" {
		t.Fatalf("subject = %q", subj)
	}
	if body != "Hi Ali, total RM 15.00 to buyer@example.com" {
		t.Fatalf("body = %q", body)
	}
}

// TestResolveNotificationRecipient covers literal, {field_id} and empty
// recipients (admin_email resolution needs a real UserStore, covered by the
// integration path).
func TestResolveNotificationRecipient(t *testing.T) {
	svc := &Service{}
	ref := NotificationRef{Fields: map[string]string{"email": "buyer@example.com"}}
	cases := []struct {
		name, to, want string
	}{
		{"literal", "admin@example.com", "admin@example.com"},
		{"field id", "{email}", "buyer@example.com"},
		{"field id missing", "{missing}", ""},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := svc.resolveNotificationRecipient(context.Background(), ref, c.to); got != c.want {
				t.Fatalf("resolve(%q) = %q, want %q", c.to, got, c.want)
			}
		})
	}
}
