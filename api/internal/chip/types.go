// Package chip implements a minimal client for the CHIP Collect API
// (one-time purchases, card tokenization for recurring charges, webhook
// signature verification). Resource paths are PLURAL with trailing slash
// per CHIP's documented convention.
package chip

// ClientDetails mirrors CHIP ClientDetailsBody (subset used by JomForm).
type ClientDetails struct {
	Email           string `json:"email,omitempty"`
	FullName        string `json:"full_name,omitempty"`
	Phone           string `json:"phone,omitempty"`
	Country         string `json:"country,omitempty"`
	StreetAddress   string `json:"street_address,omitempty"`
	City            string `json:"city,omitempty"`
	ZipCode         string `json:"zip_code,omitempty"`
	State           string `json:"state,omitempty"`
	LegalName       string `json:"legal_name,omitempty"`
	RegistrationNum string `json:"registration_number,omitempty"`
	TaxNumber       string `json:"tax_number,omitempty"`
}

// Product mirrors CHIP ProductBody. Price is an integer amount in SEN.
// Quantity is a string per CHIP's API (e.g. "2").
type Product struct {
	Name     string `json:"name"`
	Price    int64  `json:"price"`
	Quantity string `json:"quantity,omitempty"`
	Category string `json:"category,omitempty"`
}

// PurchaseDetails mirrors CHIP PurchaseDetailsBody (subset).
type PurchaseDetails struct {
	Products        []Product         `json:"products,omitempty"`
	Currency        string            `json:"currency,omitempty"`
	Reference       string            `json:"reference,omitempty"`
	SuccessRedirect string            `json:"success_redirect,omitempty"`
	FailureRedirect string            `json:"failure_redirect,omitempty"`
	CancelRedirect  string            `json:"cancel_redirect,omitempty"`
	SuccessCallback string            `json:"success_callback,omitempty"`
	TotalOverride   *int64            `json:"total_override,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	CreatorAgent    string            `json:"creator_agent,omitempty"`
}

// PurchaseRequest is the POST /purchases/ body.
type PurchaseRequest struct {
	Client         *ClientDetails  `json:"client,omitempty"`
	Purchase       PurchaseDetails `json:"purchase"`
	BrandID        string          `json:"brand_id"`
	ClientID       *string         `json:"client_id,omitempty"`
	ForceRecurring bool            `json:"force_recurring,omitempty"`
	SendReceipt    bool            `json:"send_receipt,omitempty"`
}

// Purchase is the response/callback Purchase object (subset).
type Purchase struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	CheckoutURL      string `json:"checkout_url"`
	Reference        string `json:"reference"`
	Total            int64  `json:"total"`
	Currency         string `json:"currency"`
	EventType        string `json:"event_type,omitempty"`
	IsRecurringToken bool   `json:"is_recurring_token"`
	RecurringToken   string `json:"recurring_token,omitempty"`
	CreatedOn        int64  `json:"created_on,omitempty"`
	UpdatedOn        int64  `json:"updated_on,omitempty"`
}

// ChargeRequest is the POST /purchases/{id}/charge/ body.
type ChargeRequest struct {
	RecurringToken string `json:"recurring_token"`
}

// CHIP purchase statuses relevant to JomForm.
const (
	StatusCreated       = "created"
	StatusPaid          = "paid"
	StatusError         = "error"
	StatusPendingCharge = "pending_charge"
	StatusRefunded      = "refunded"
)

// Event types subscribed by JomForm.
const (
	EventCreated        = "purchase.created"
	EventPaid           = "purchase.paid"
	EventPaymentFailure = "purchase.payment_failure"
	EventPendingCharge  = "purchase.pending_charge"
	EventTokenDeleted   = "purchase.recurring_token_deleted"
	EventPendingTokDel  = "purchase.pending_recurring_token_delete"
)
