package respkit

import (
	"path"
	"sync"

	"github.com/lee87902407/respkit/internal/protocol"
)

type pubSubWriter interface {
	WriteValue(protocol.RespValue) error
	Flush() error
}

type pubSubClient struct {
	writer pubSubWriter
	mu     sync.Mutex
}

type pubSubEntry struct {
	pattern bool
	value   string
	client  *pubSubClient
}

// PubSub provides Redis-compatible publish/subscribe primitives.
type PubSub struct {
	mu      sync.RWMutex
	clients map[Conn]*pubSubClient
	entries map[Conn]map[pubSubEntry]struct{}
}

// NewPubSub creates an empty pub/sub registry.
func NewPubSub() *PubSub {
	return &PubSub{
		clients: make(map[Conn]*pubSubClient),
		entries: make(map[Conn]map[pubSubEntry]struct{}),
	}
}

func (ps *PubSub) ensureClient(conn Conn) *pubSubClient {
	if client, ok := ps.clients[conn]; ok {
		return client
	}
	client := &pubSubClient{writer: &legacyPubSubWriter{conn: conn}}
	ps.clients[conn] = client
	ps.entries[conn] = make(map[pubSubEntry]struct{})
	return client
}

// Subscribe subscribes a connection to a channel.
func (ps *PubSub) Subscribe(conn Conn, channel string) error {
	ps.mu.Lock()
	client := ps.ensureClient(conn)
	entry := pubSubEntry{pattern: false, value: channel, client: client}
	ps.entries[conn][entry] = struct{}{}
	count := len(ps.entries[conn])
	ps.mu.Unlock()

	client.mu.Lock()
	defer client.mu.Unlock()
	if err := client.writer.WriteValue(buildSubscribeAckValue("subscribe", channel, count)); err != nil {
		return err
	}
	return client.writer.Flush()
}

// PSubscribe subscribes a connection to a glob pattern.
func (ps *PubSub) PSubscribe(conn Conn, pattern string) error {
	ps.mu.Lock()
	client := ps.ensureClient(conn)
	entry := pubSubEntry{pattern: true, value: pattern, client: client}
	ps.entries[conn][entry] = struct{}{}
	count := len(ps.entries[conn])
	ps.mu.Unlock()

	client.mu.Lock()
	defer client.mu.Unlock()
	if err := client.writer.WriteValue(buildSubscribeAckValue("psubscribe", pattern, count)); err != nil {
		return err
	}
	return client.writer.Flush()
}

// Unsubscribe unsubscribes a connection from a channel.
func (ps *PubSub) Unsubscribe(conn Conn, channel string) error {
	return ps.unsubscribe(conn, false, channel)
}

// PUnsubscribe unsubscribes a connection from a glob pattern.
func (ps *PubSub) PUnsubscribe(conn Conn, pattern string) error {
	return ps.unsubscribe(conn, true, pattern)
}

func (ps *PubSub) unsubscribe(conn Conn, pattern bool, value string) error {
	ps.mu.Lock()
	client, ok := ps.clients[conn]
	if !ok {
		ps.mu.Unlock()
		return nil
	}
	for entry := range ps.entries[conn] {
		if entry.pattern == pattern && entry.value == value {
			delete(ps.entries[conn], entry)
		}
	}
	count := len(ps.entries[conn])
	if count == 0 {
		delete(ps.entries, conn)
		delete(ps.clients, conn)
	}
	ps.mu.Unlock()

	name := "unsubscribe"
	if pattern {
		name = "punsubscribe"
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if err := client.writer.WriteValue(buildSubscribeAckValue(name, value, count)); err != nil {
		return err
	}
	return client.writer.Flush()
}

// Publish broadcasts a message to all matching subscribers.
func (ps *PubSub) Publish(channel, message string) (int, error) {
	ps.mu.RLock()
	matches := make([]pubSubEntry, 0)
	for _, entries := range ps.entries {
		for entry := range entries {
			if entry.pattern {
				ok, err := path.Match(entry.value, channel)
				if err != nil || !ok {
					continue
				}
			} else if entry.value != channel {
				continue
			}
			matches = append(matches, entry)
		}
	}
	ps.mu.RUnlock()

	sent := 0
	for _, entry := range matches {
		entry.client.mu.Lock()
		if entry.pattern {
			if err := entry.client.writer.WriteValue(buildPatternMessageValue(entry.value, channel, message)); err != nil {
				entry.client.mu.Unlock()
				return sent, err
			}
		} else {
			if err := entry.client.writer.WriteValue(buildPublishMessageValue(channel, message)); err != nil {
				entry.client.mu.Unlock()
				return sent, err
			}
		}
		if err := entry.client.writer.Flush(); err != nil {
			entry.client.mu.Unlock()
			return sent, err
		}
		entry.client.mu.Unlock()
		sent++
	}
	return sent, nil
}

func buildSubscribeAckValue(kind, value string, count int) protocol.RespValue {
	return protocol.ArrayOf(
		protocol.BulkFromString(kind),
		protocol.BulkFromString(value),
		protocol.Integer(int64(count)),
	)
}

func buildPublishMessageValue(channel, message string) protocol.RespValue {
	return protocol.ArrayOf(
		protocol.BulkFromString("message"),
		protocol.BulkFromString(channel),
		protocol.BulkFromString(message),
	)
}

func buildPatternMessageValue(pattern, channel, message string) protocol.RespValue {
	return protocol.ArrayOf(
		protocol.BulkFromString("pmessage"),
		protocol.BulkFromString(pattern),
		protocol.BulkFromString(channel),
		protocol.BulkFromString(message),
	)
}

type legacyPubSubWriter struct {
	conn Conn
}

func (w *legacyPubSubWriter) WriteValue(value protocol.RespValue) error {
	return writeLegacyValue(w.conn, value)
}

func (w *legacyPubSubWriter) Flush() error {
	return w.conn.Flush()
}

func writeLegacyValue(conn Conn, value protocol.RespValue) error {
	switch value.Type {
	case protocol.TypeArray:
		if err := conn.WriteArray(len(value.Array)); err != nil {
			return err
		}
		for _, elem := range value.Array {
			if err := writeLegacyValue(conn, elem); err != nil {
				return err
			}
		}
		return nil
	case protocol.TypeBulkString:
		if value.Bulk == nil {
			return conn.WriteNull()
		}
		return conn.WriteBulk(value.Bulk)
	case protocol.TypeInteger:
		return conn.WriteInt(value.Int)
	case protocol.TypeSimpleString:
		return conn.WriteString(value.Str)
	case protocol.TypeError:
		return conn.WriteError(value.Str)
	case protocol.TypeNull:
		return conn.WriteNull()
	default:
		return conn.WriteAny(value)
	}
}
