// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
package main

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// ErrNoChatAddress reports an inbound message that names no chat identity. The
// refusal is the whole answer: a message without an address is never routed to
// a session that belongs to somebody else.
var ErrNoChatAddress = errors.New("wechat-bot: the inbound message names no chat identity")

// ErrSessionPrefixMissing reports a router constructed without the explicit
// session prefix its address spelling starts with.
var ErrSessionPrefixMissing = errors.New("wechat-bot: the session router carries no session prefix")

// ErrTargetRebind reports a second chat identity claimed for an address that
// already delivered to another chat. The bound target is the delivery identity
// of that session and is never rewritten by a later claim.
var ErrTargetRebind = errors.New("wechat-bot: the address is already bound to another chat identity")

// SessionRouter maps one explicit chat identity to the session address the
// runtime is driven at, and holds the delivery identity of each address it
// admitted.
//
// The mapping is injective in its spelling: one chat identity has exactly one
// session identity and two chat identities never share one, so a restart
// re-derives the same addresses instead of inventing a fresh history.
type SessionRouter struct {
	agentID event.AgentID
	prefix  string

	mu      sync.Mutex
	seq     uint64
	targets map[event.Address]string
	sent    map[event.Address]int
}

// NewSessionRouter returns the router of one declared agent beside the explicit
// session prefix its addresses are spelled with. A missing agent or prefix is
// refused at construction, so no address is ever built from a default.
func NewSessionRouter(agentID event.AgentID, prefix string) (*SessionRouter, error) {
	if agentID == "" {
		return nil, ErrNoChatAddress
	}
	if prefix == "" {
		return nil, ErrSessionPrefixMissing
	}
	return &SessionRouter{
		agentID: agentID,
		prefix:  prefix,
		targets: map[event.Address]string{},
		sent:    map[event.Address]int{},
	}, nil
}

// AddressFor returns the explicit address of one chat identity: the declared
// agent beside the session identity that identity spells. The chat identity is
// percent-escaped, so the spelling is injective over every byte a channel may
// carry and two chats never collapse onto one session.
func (r *SessionRouter) AddressFor(chatID string) (event.Address, error) {
	trimmed := strings.TrimSpace(chatID)
	if trimmed == "" {
		return event.Address{}, ErrNoChatAddress
	}
	escaped := url.PathEscape(trimmed)
	if escaped == "" {
		return event.Address{}, ErrNoChatAddress
	}
	return event.Address{AgentID: r.agentID, SessionID: event.SessionID(r.prefix + escaped)}, nil
}

// Bind records the chat identity a drive was admitted for as the delivery
// identity of its address: a later explicit send from that address reaches the
// chat its own drive came from. A rebound address keeps its first identity and
// reports the conflict instead of silently retargeting the session.
func (r *SessionRouter) Bind(addr event.Address, chatID string) error {
	trimmed := strings.TrimSpace(chatID)
	if trimmed == "" {
		return ErrNoChatAddress
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if bound, ok := r.targets[addr]; ok && bound != trimmed {
		return fmt.Errorf("%w: %s", ErrTargetRebind, addr.SessionID)
	}
	r.targets[addr] = trimmed
	return nil
}

// Target reports the chat identity bound to exactly this address. An address no
// drive ever bound answers not-ok: the delivery face then refuses instead of
// resolving a most-recent chat of another session.
func (r *SessionRouter) Target(addr event.Address) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	chatID, ok := r.targets[addr]
	return chatID, ok
}

// BoundAddresses reports the addresses of the named agent that a drive bound to
// a chat identity, in session order. The reflection delivery resolves its declared
// target through this face: an agent no drive reached holds no address, so its
// result is withheld rather than sent to a session of another agent.
func (r *SessionRouter) BoundAddresses(agentID event.AgentID) []event.Address {
	if agentID == "" || agentID != r.agentID {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	addresses := make([]event.Address, 0, len(r.targets))
	for addr := range r.targets {
		addresses = append(addresses, addr)
	}
	sort.Slice(addresses, func(i, j int) bool { return addresses[i].SessionID < addresses[j].SessionID })
	return addresses
}

// NextInvocation mints the responsibility range identity of one drive of the
// address. The identity is unique per drive and derived from the address, so a
// range never collides with the range of another session.
func (r *SessionRouter) NextInvocation(addr event.Address) event.InvocationID {
	r.mu.Lock()
	r.seq++
	n := r.seq
	r.mu.Unlock()
	return event.InvocationID(string(addr.SessionID) + "#" + strconv.FormatUint(n, 10))
}

// TakeUndelivered returns the transcript messages the address accumulated since
// the previous take and records them as consumed. Delivery reads one session's
// own transcript only, so a reply never carries another session's turn.
func (r *SessionRouter) TakeUndelivered(addr event.Address, history []model.Message) []model.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	from := r.sent[addr]
	if from > len(history) {
		from = len(history)
	}
	fresh := append([]model.Message(nil), history[from:]...)
	r.sent[addr] = len(history)
	return fresh
}
