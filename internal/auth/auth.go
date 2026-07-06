package auth

// Mode constants define a credential's scope. Handlers enforce Mode at the
// engine/handler layer (read-only keys cannot ingest; write-only keys cannot
// query; admin can manage tokens).
const (
	ModeRead  = "read"  // queries only
	ModeWrite = "write" // ingest + queries
	ModeAdmin = "admin" // full access, including token management
)

// Source constants describe how a request was authenticated. Carried on
// Principal.Source for audit and for the loopback-bypass path (spec 045 US5).
const (
	SourceAPIKey  = "apikey"  // a gorag_ API key
	SourceSession = "session" // a gorags_ session token (UI login)
	SourceBypass  = "bypass"  // loopback peer + empty credential stores
)

// Principal is the authenticated caller. Validate returns it; transport
// middleware puts it in context.Context for handlers to enforce Mode.
type Principal struct {
	Subject string // APIKey.ID (e.g. "gorag_ab12cd34") or AdminUser.Username
	Mode    string // ModeRead | ModeWrite | ModeAdmin
	Source  string // SourceAPIKey | SourceSession | SourceBypass
}
