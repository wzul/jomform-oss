// The DASHBOARD language of one chat request (card t_ec87d6c7).
//
// WHY THIS EXISTS. Every word on the confirmation card is either a dictionary
// key the page resolves or the merchant's own data — except ONE: propose_write's
// `reason`, which the MODEL writes and the card shows under its fields. That
// sentence therefore follows whatever language the model was told to use, and the
// model used to be told "the language of the conversation". A merchant on an
// ENGLISH dashboard who asked in Malay got a Malay sentence on an English card —
// half a card in each language, on the surface where the wording IS the safety
// mechanism, which is the rule Wan states as "if the UI is English, this cannot
// be Malay".
//
// WHY THE CONTEXT AND NOT A Runner FIELD OR A Run ARGUMENT.
//
//   - Not a Runner field: one Runner serves every request, so a per-request
//     language on the struct would be shared state that two concurrent merchants
//     write and read. That is a data race that only shows up with two locales.
//   - Not a Run parameter: `Run` is the ChatRunner interface method the REST lane
//     and the test fakes both implement; the language is not part of the chat's
//     own contract (a runner does not decide the dashboard's language), and this
//     package already receives its per-request identity through the context via
//     mcpsrv (IdentityFromContext).
//
// So the REST handler, which is the layer that knows WHO is calling, attaches the
// value and Run reads it once when it builds the system prompt. Absent means "not
// set", which resolves to English — the dashboard's own default.
package chat

import "context"

type dashboardLangCtxKey struct{}

// WithDashboardLang records the caller's dashboard UI language on the request
// context. The value is a profile language code ("en", "ms", "zh", "ta", "ar");
// "" and any other value are valid and mean "not set" — the prompt resolves both
// to English, the language the dashboard itself falls back to.
func WithDashboardLang(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, dashboardLangCtxKey{}, lang)
}

// DashboardLangFrom returns the dashboard language attached by WithDashboardLang
// ("" when none was). Its only consumer is the system prompt, where the model is
// told which language to write the card's own sentence in.
func DashboardLangFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	lang, _ := ctx.Value(dashboardLangCtxKey{}).(string)
	return lang
}
