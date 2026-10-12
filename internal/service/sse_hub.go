// Package service — SSE (Server-Sent Events) 事件流服务。
package service

import (
	"encoding/json"

	"go.uber.org/zap"
)

// SSEHub 管理 SSE 客户端连接和事件广播。
type SSEHub struct {
	clients    map[chan SSEEvent]bool
	broadcast  chan SSEEvent
	register   chan chan SSEEvent
	unregister chan chan SSEEvent
	log        *zap.Logger
	stopCh     chan struct{}
}

type SSEEvent struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// SSEEvent 事件类型常量。
const (
	EventTypeScan   = "scan"
	EventTypeTask   = "task"
	EventTypeSystem = "system"
	EventTypeAuth   = "auth"
)

const EventTypePlayerRequestLog = "player_request_log_changed"

// NewSSEHub 创建 SSE Hub 实例。
func NewSSEHub(log *zap.Logger) *SSEHub {
	return &SSEHub{
		clients:    make(map[chan SSEEvent]bool),
		broadcast:  make(chan SSEEvent, 256),
		register:   make(chan chan SSEEvent),
		unregister: make(chan chan SSEEvent),
		log:        log,
		stopCh:     make(chan struct{}),
	}
}

// Run 启动 SSE Hub 的事件循环。
func (h *SSEHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			h.log.Debug("SSE client connected", zap.Int("total", len(h.clients)))

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client)
				h.log.Debug("SSE client disconnected", zap.Int("total", len(h.clients)))
			}

		case event := <-h.broadcast:
			h.distribute(event)

		case <-h.stopCh:
			h.closeAll()
			return
		}
	}
}

// Stop 停止 SSE Hub。
func (h *SSEHub) Stop() {
	close(h.stopCh)
}

// ClientChannel SSE 客户端通道包装器。
type ClientChannel struct {
	Ch chan SSEEvent
}

// Subscribe 注册一个新的 SSE 客户端，返回 SSE 客户端包装器。
func (h *SSEHub) Subscribe() *ClientChannel {
	ch := make(chan SSEEvent, 100)
	h.register <- ch
	return &ClientChannel{Ch: ch}
}

// Unsubscribe 取消注册 SSE 客户端。
func (h *SSEHub) Unsubscribe(client *ClientChannel) {
	if client != nil && client.Ch != nil {
		h.unregister <- client.Ch
	}
}

// Broadcast 向所有连接的客户端广播事件。
func (h *SSEHub) Broadcast(eventType string, payload interface{}) {
	event := SSEEvent{
		Type:    eventType,
		Payload: payload,
	}
	select {
	case h.broadcast <- event:
	default:
		h.log.Warn("SSE broadcast queue full, dropping event", zap.String("type", eventType))
	}
}

// SendToUser 向指定用户发送事件（通过 UserID 匹配）。
// 注意：此方法需要在客户端连接时关联 UserID。
func (h *SSEHub) SendToUser(userID string, eventType string, payload interface{}) {
	// 目前通过广播实现，未来可扩展为按用户分组
	h.Broadcast(eventType, payload)
}

// distribute 将事件分发给所有客户端。
func (h *SSEHub) distribute(event SSEEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		h.log.Error("failed to marshal SSE event", zap.Error(err))
		return
	}

	for client := range h.clients {
		select {
		case client <- event:
		default:
			// 客户端通道已满，跳过
			h.log.Warn("SSE client buffer full", zap.String("event", string(data)))
		}
	}
}

// closeAll 关闭所有客户端连接。
func (h *SSEHub) closeAll() {
	for client := range h.clients {
		close(client)
	}
	h.clients = make(map[chan SSEEvent]bool)
}
