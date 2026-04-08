package respkit

import (
	"path"
	"sync"
)

type pubSubClient struct {
	conn DetachedConn
	mu   sync.Mutex
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
	detached := conn.Detach()
	client := &pubSubClient{conn: detached}
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
	if err := client.conn.WriteArray(3); err != nil {
		return err
	}
	if err := client.conn.WriteBulk([]byte("subscribe")); err != nil {
		return err
	}
	if err := client.conn.WriteBulk([]byte(channel)); err != nil {
		return err
	}
	if err := client.conn.WriteInt(int64(count)); err != nil {
		return err
	}
	return client.conn.Flush()
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
	if err := client.conn.WriteArray(3); err != nil {
		return err
	}
	if err := client.conn.WriteBulk([]byte("psubscribe")); err != nil {
		return err
	}
	if err := client.conn.WriteBulk([]byte(pattern)); err != nil {
		return err
	}
	if err := client.conn.WriteInt(int64(count)); err != nil {
		return err
	}
	return client.conn.Flush()
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
	if err := client.conn.WriteArray(3); err != nil {
		return err
	}
	if err := client.conn.WriteBulk([]byte(name)); err != nil {
		return err
	}
	if err := client.conn.WriteBulk([]byte(value)); err != nil {
		return err
	}
	if err := client.conn.WriteInt(int64(count)); err != nil {
		return err
	}
	return client.conn.Flush()
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
			if err := entry.client.conn.WriteArray(4); err != nil {
				entry.client.mu.Unlock()
				return sent, err
			}
			if err := entry.client.conn.WriteBulk([]byte("pmessage")); err != nil {
				entry.client.mu.Unlock()
				return sent, err
			}
			if err := entry.client.conn.WriteBulk([]byte(entry.value)); err != nil {
				entry.client.mu.Unlock()
				return sent, err
			}
			if err := entry.client.conn.WriteBulk([]byte(channel)); err != nil {
				entry.client.mu.Unlock()
				return sent, err
			}
			if err := entry.client.conn.WriteBulk([]byte(message)); err != nil {
				entry.client.mu.Unlock()
				return sent, err
			}
		} else {
			if err := entry.client.conn.WriteArray(3); err != nil {
				entry.client.mu.Unlock()
				return sent, err
			}
			if err := entry.client.conn.WriteBulk([]byte("message")); err != nil {
				entry.client.mu.Unlock()
				return sent, err
			}
			if err := entry.client.conn.WriteBulk([]byte(channel)); err != nil {
				entry.client.mu.Unlock()
				return sent, err
			}
			if err := entry.client.conn.WriteBulk([]byte(message)); err != nil {
				entry.client.mu.Unlock()
				return sent, err
			}
		}
		if err := entry.client.conn.Flush(); err != nil {
			entry.client.mu.Unlock()
			return sent, err
		}
		entry.client.mu.Unlock()
		sent++
	}
	return sent, nil
}
