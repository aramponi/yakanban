package core

import (
	"context"
	"fmt"
)

// CapabilitySet is resolved for a board and caller, rather than for an adapter
// type. A missing bit always means unsupported; Reasons explains the backend
// restriction (tier, permission or mapping), without guessing a licence.
type CapabilitySet struct {
	Supported Capability            `json:"supported"`
	Reasons   map[Capability]string `json:"reasons,omitempty"`
}

func (s CapabilitySet) Has(c Capability) bool { return s.Supported.Has(c) }

var capabilityNames = []struct {
	bit  Capability
	name string
}{
	{CapClaims, "claims"}, {CapDependencies, "dependencies"}, {CapParent, "parent"},
	{CapBlocked, "blocked reason"}, {CapEstimate, "estimate"}, {CapClass, "class"},
	{CapDueDate, "due date"}, {CapDelete, "delete"}, {CapArchive, "archive"},
	{CapLinkedBranch, "linked branches"}, {CapWorkflowDates, "workflow dates"},
}

// CapabilityStatus is the inspectable, named representation used by config.
type CapabilityStatus struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

func CapabilityNames() []string {
	names := make([]string, 0, len(capabilityNames))
	for _, c := range capabilityNames {
		names = append(names, c.name)
	}
	return names
}

func (s CapabilitySet) reason(c Capability, name string) string {
	if r := s.Reasons[c]; r != "" {
		return r
	}
	return "this backend does not support " + name
}

func (s CapabilitySet) Statuses() map[string]CapabilityStatus {
	result := make(map[string]CapabilityStatus, len(capabilityNames))
	for _, c := range capabilityNames {
		status := CapabilityStatus{Supported: s.Has(c.bit)}
		if !status.Supported {
			status.Reason = s.reason(c.bit, c.name)
		}
		result[c.name] = status
	}
	return result
}

// Require returns a stable sentinel and the provider's actionable explanation.
func (s CapabilitySet) Require(provider string, want Capability) error {
	for _, c := range capabilityNames {
		if want.Has(c.bit) && !s.Has(c.bit) {
			return fmt.Errorf("%w: provider %s: %s", ErrUnsupported, provider, s.reason(c.bit, c.name))
		}
	}
	return nil
}

// ResolveCapabilities uses the same live/cached metadata as the board. Runtime
// resolution failures remain errors, never an empty set or a guessed tier.
func ResolveCapabilities(ctx context.Context, p Provider) (CapabilitySet, error) {
	board, err := p.Board(ctx)
	if err != nil {
		return CapabilitySet{}, err
	}
	if board == nil {
		return CapabilitySet{}, fmt.Errorf("provider %s returned no board metadata", p.Name())
	}
	if board.Capabilities != nil {
		return *board.Capabilities, nil
	}
	return LegacyCapabilities(p), nil
}

// LegacyCapabilities supports adapters written before board capabilities were
// introduced. New adapters must return BoardInfo.Capabilities. Workflow date
// writes were implicit in the old port; optional branch support used Brancher.
func LegacyCapabilities(p Provider) CapabilitySet {
	if wrapper, ok := p.(Unwrapper); ok {
		return LegacyCapabilities(wrapper.Unwrap())
	}
	if legacy, ok := p.(interface{ Capabilities() Capability }); ok {
		bits := legacy.Capabilities() | CapWorkflowDates
		if _, ok := AsBrancher(p); ok {
			bits |= CapLinkedBranch
		}
		return CapabilitySet{Supported: bits}
	}
	return CapabilitySet{}
}
