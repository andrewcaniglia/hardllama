//go:build windows || darwin

package responses

import (
	"encoding/json"
	"time"

	"github.com/ollama/ollama/app/store"
	"github.com/ollama/ollama/types/model"
)

type ChatInfo struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	UserExcerpt string    `json:"userExcerpt"`
	CreatedAt   time.Time `json:"createdAt" ts_type:"Date" ts_transform:"new Date(__VALUE__)"`
	UpdatedAt   time.Time `json:"updatedAt" ts_type:"Date" ts_transform:"new Date(__VALUE__)"`
	Private     bool      `json:"private,omitempty"`
}

type ChatsResponse struct {
	ChatInfos []ChatInfo `json:"chatInfos"`
}

type ChatResponse struct {
	Chat store.Chat `json:"chat"`
}

type Model struct {
	Model      string     `json:"model"`
	Digest     string     `json:"digest,omitempty"`
	ModifiedAt *time.Time `json:"modified_at,omitempty"`
}

type ModelsResponse struct {
	Models []Model `json:"models"`
}

type InferenceCompute struct {
	Library string `json:"library"`
	Variant string `json:"variant"`
	Compute string `json:"compute"`
	Driver  string `json:"driver"`
	Name    string `json:"name"`
	VRAM    string `json:"vram"`
}

type InferenceComputeResponse struct {
	InferenceComputes    []InferenceCompute `json:"inferenceComputes"`
	DefaultContextLength int                `json:"defaultContextLength"`
}

type ModelCapabilitiesResponse struct {
	Capabilities []model.Capability `json:"capabilities"`
}

// ChatEvent is for regular chat messages and assistant interactions
type ChatEvent struct {
	EventName string `json:"eventName" ts_type:"\"chat\" | \"thinking\" | \"assistant_with_tools\" | \"tool_call\" | \"tool\" | \"tool_result\" | \"done\" | \"chat_created\""`

	// Chat/Assistant message fields
	Content           *string    `json:"content,omitempty"`
	Thinking          *string    `json:"thinking,omitempty"`
	ThinkingTimeStart *time.Time `json:"thinkingTimeStart,omitempty" ts_type:"Date | undefined" ts_transform:"__VALUE__ && new Date(__VALUE__)"`
	ThinkingTimeEnd   *time.Time `json:"thinkingTimeEnd,omitempty" ts_type:"Date | undefined" ts_transform:"__VALUE__ && new Date(__VALUE__)"`

	// Tool-related fields
	ToolCalls      []store.ToolCall `json:"toolCalls,omitempty"`
	ToolCall       *store.ToolCall  `json:"toolCall,omitempty"`
	ToolName       *string          `json:"toolName,omitempty"`
	ToolResult     *bool            `json:"toolResult,omitempty"`
	ToolResultData any              `json:"toolResultData,omitempty"`

	// Chat creation fields
	ChatID *string `json:"chatId,omitempty"`

	// Tool state field from the new code
	ToolState any `json:"toolState,omitempty"`
}

// DownloadEvent is for model download progress
type DownloadEvent struct {
	EventName string `json:"eventName" ts_type:"\"download\""`
	Total     int64  `json:"total" ts_type:"number"`
	Completed int64  `json:"completed" ts_type:"number"`
	Done      bool   `json:"done" ts_type:"boolean"`
}

// ErrorEvent is for error messages
type ErrorEvent struct {
	EventName string `json:"eventName" ts_type:"\"error\""`
	Error     string `json:"error"`
	Code      string `json:"code,omitempty"`    // Optional error code for different error types
	Details   string `json:"details,omitempty"` // Optional additional details
}

type SettingsResponse struct {
	Settings store.Settings `json:"settings"`
}

type HealthResponse struct {
	Healthy bool `json:"healthy"`
}

type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Bio       string `json:"bio,omitempty"`
	AvatarURL string `json:"avatarurl,omitempty"`
	FirstName string `json:"firstname,omitempty"`
	LastName  string `json:"lastname,omitempty"`
	Plan      string `json:"plan,omitempty"`
}

type Attachment struct {
	Filename string `json:"filename"`
	Data     string `json:"data,omitempty"` // omitempty = optional, no data = existing file reference
}

type ChatRequest struct {
	FolderSnapshot json.RawMessage `json:"folderSnapshot,omitempty"`
	FolderContext  string          `json:"folderContext,omitempty"`
	Model          string          `json:"model"`
	Prompt         string          `json:"prompt"`
	Index          *int            `json:"index,omitempty"`
	Attachments    []Attachment    `json:"attachments,omitempty"`
	WebSearch      *bool           `json:"web_search,omitempty"`
	FileTools      *bool           `json:"file_tools,omitempty"`
	ForceUpdate    bool            `json:"forceUpdate,omitempty"`
	Think          any             `json:"think,omitempty"`
	Private        bool            `json:"private,omitempty"`
}

type Error struct {
	Error string `json:"error"`
}

// ModelServerResponse is the paired computer that runs models. An empty
// address means this one.
type ModelServerResponse struct {
	Address string `json:"address"`
}

// ShareResponse describes sharing this computer's models.
type ShareResponse struct {
	Enabled   bool          `json:"enabled"`
	Running   bool          `json:"running"`
	Port      string        `json:"port"`
	Addresses []string      `json:"addresses"`
	Devices   []ShareDevice `json:"devices"`
}

// ShareDevice is a device paired with this computer.
type ShareDevice struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// PairingCodeResponse is a new code for pairing a device.
type PairingCodeResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

type ModelUpstreamResponse struct {
	Stale bool   `json:"stale"`
	Error string `json:"error,omitempty"`
}

// Serializable data for the browser state
type BrowserStateData struct {
	PageStack  []string         `json:"page_stack"`  // Sequential list of page URLs
	ViewTokens int              `json:"view_tokens"` // Number of tokens to show in viewport
	URLToPage  map[string]*Page `json:"url_to_page"` // URL to page contents
}

// Page represents the contents of a page
type Page struct {
	URL       string         `json:"url"`
	Title     string         `json:"title"`
	Text      string         `json:"text"`
	Lines     []string       `json:"lines"`
	Links     map[int]string `json:"links,omitempty" ts_type:"Record<number, string>"`
	FetchedAt time.Time      `json:"fetched_at"`
}

// Draft is what the user was typing in a chat when chats locked.
type Draft struct {
	Content string `json:"content"`
}

// LockDelay is how long chats stay unlocked after the user leaves HardLlama.
type LockDelay struct {
	Seconds int `json:"seconds"`
}

// ContextUsageResponse is how much of the context window a chat's last
// reply used. Usage is nil before the first reply.
type ContextUsageResponse struct {
	Usage *store.ContextUsage `json:"usage"`
	// Estimate guesses the chat's tokens, for when Usage is missing or was
	// counted with another model.
	Estimate int `json:"estimate"`
}

// ModelContextLengthResponse is the most context a model supports, 0 if
// unknown.
type ModelContextLengthResponse struct {
	Max int `json:"max"`
}

type AttachmentEstimate struct {
	Filename string `json:"filename"`
	Tokens   int    `json:"tokens"`
	Image    bool   `json:"image"`
}

type AttachmentEstimateResponse struct {
	Attachments []AttachmentEstimate `json:"attachments"`
}
