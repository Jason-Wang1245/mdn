package copilot

import "encoding/json"

// Position is an LSP position; Character counts UTF-16 code units.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is an LSP range.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Command is an LSP command.
type Command struct {
	Title     string            `json:"title,omitempty"`
	Command   string            `json:"command"`
	Arguments []json.RawMessage `json:"arguments,omitempty"`
}

// InlineCompletionItem is one suggestion returned by textDocument/inlineCompletion.
// Raw preserves the item exactly as received so it can be echoed back in
// telemetry notifications.
type InlineCompletionItem struct {
	InsertText string   `json:"insertText"`
	Range      *Range   `json:"range,omitempty"`
	Command    *Command `json:"command,omitempty"`

	Raw json.RawMessage `json:"-"`
}

// StatusKind is the kind reported by didChangeStatus.
type StatusKind string

const (
	StatusNormal   StatusKind = "Normal"
	StatusError    StatusKind = "Error"
	StatusWarning  StatusKind = "Warning"
	StatusInactive StatusKind = "Inactive"
)

// Status is the payload of the didChangeStatus notification.
type Status struct {
	Kind    StatusKind `json:"kind"`
	Message string     `json:"message"`
	Busy    bool       `json:"busy"`
}

// SignInResult is the response to the signIn request.
type SignInResult struct {
	Status          string   `json:"status,omitempty"` // e.g. "AlreadySignedIn", "OK", "PromptUserDeviceFlow"
	User            string   `json:"user,omitempty"`
	UserCode        string   `json:"userCode,omitempty"`
	VerificationURI string   `json:"verificationUri,omitempty"`
	ExpiresIn       int      `json:"expiresIn,omitempty"`
	Interval        int      `json:"interval,omitempty"`
	Command         *Command `json:"command,omitempty"`
}

// AlreadySignedIn reports whether no device flow is needed.
func (r SignInResult) AlreadySignedIn() bool {
	return r.Status == "AlreadySignedIn" || r.Status == "OK" || (r.UserCode == "" && r.User != "")
}

// MessageActionItem is an action button for window/showMessageRequest.
type MessageActionItem struct {
	Title string `json:"title"`
}

// ShowMessageRequest is the payload of window/showMessageRequest.
type ShowMessageRequest struct {
	Type    int                 `json:"type"`
	Message string              `json:"message"`
	Actions []MessageActionItem `json:"actions,omitempty"`
}
