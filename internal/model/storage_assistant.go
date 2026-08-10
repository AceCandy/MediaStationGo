package model

// AssistantSession groups a multi-turn chat with the AI assistant.
type AssistantSession struct {
	Base
	UserID string `gorm:"index;size:36;not null" json:"user_id"`
	Title  string `gorm:"size:255" json:"title,omitempty"`
}

// AssistantMessage is one entry in an AssistantSession transcript.
//
// Role is "user" | "assistant" | "system".  The optional OperationID
// links a message to an action the assistant proposed (so the UI can
// offer Undo).
type AssistantMessage struct {
	Base
	SessionID   string `gorm:"index;size:36;not null" json:"session_id"`
	Role        string `gorm:"size:16;not null" json:"role"`
	Content     string `gorm:"type:text;not null" json:"content"`
	OperationID string `gorm:"size:36" json:"operation_id,omitempty"`
}
