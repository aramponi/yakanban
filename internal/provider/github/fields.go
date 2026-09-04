package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

// updateKind says how a field value must be written.
type updateKind int

const (
	updateText updateKind = iota
	updateDate
	updateSelect
	updateClear
)

type fieldUpdate struct {
	Field string
	Kind  updateKind
	Text  string
	Date  time.Time
}

// fieldUpdates collects the project-side half of a write.
type fieldUpdates []fieldUpdate

func (u *fieldUpdates) setText(name, v string) {
	if strings.TrimSpace(v) == "" {
		return
	}
	*u = append(*u, fieldUpdate{Field: name, Kind: updateText, Text: v})
}

func (u *fieldUpdates) setSelect(name, v string) {
	if strings.TrimSpace(v) == "" {
		return
	}
	*u = append(*u, fieldUpdate{Field: name, Kind: updateSelect, Text: v})
}

func (u *fieldUpdates) setDate(name string, t *time.Time) {
	if t == nil {
		return
	}
	*u = append(*u, fieldUpdate{Field: name, Kind: updateDate, Date: *t})
}

func (u *fieldUpdates) clear(name string) {
	*u = append(*u, fieldUpdate{Field: name, Kind: updateClear})
}

// setTextOrClear writes v, or clears the field when v is empty. Used for the
// fields where "" is a meaningful value (unblocking, dropping a parent).
func (u *fieldUpdates) setTextOrClear(name, v string) {
	if strings.TrimSpace(v) == "" {
		u.clear(name)
		return
	}
	u.setText(name, v)
}

// projectPatch turns a domain patch into the list of project field writes.
func projectPatch(cur *core.Task, p core.Patch) fieldUpdates {
	var u fieldUpdates
	if p.Status != nil {
		u.setSelect(fieldStatus, *p.Status)
	}
	if p.Priority != nil {
		u.setSelect(fieldPriority, *p.Priority)
	}
	if p.Class != nil {
		u.setSelect(fieldClass, *p.Class)
	}
	if p.Estimate != nil {
		u.setTextOrClear(fieldEstimate, *p.Estimate)
	}
	if p.Blocked != nil {
		u.setTextOrClear(fieldBlocked, *p.Blocked)
	}
	if p.Parent != nil {
		u.setTextOrClear(fieldParent, *p.Parent)
	}
	if p.ClearParent {
		u.clear(fieldParent)
	}
	if p.Due != nil {
		u.setDate(fieldDue, p.Due)
	}
	if p.ClearDue {
		u.clear(fieldDue)
	}
	if p.Started != nil {
		u.setDate(fieldStarted, p.Started)
	}
	if p.ClearStarted {
		u.clear(fieldStarted)
	}
	if p.Completed != nil {
		u.setDate(fieldCompleted, p.Completed)
	}
	if p.ClearCompleted {
		u.clear(fieldCompleted)
	}
	if p.ClearDeps {
		u.clear(fieldDependsOn)
	} else if len(p.AddDeps) > 0 || len(p.RemoveDeps) > 0 {
		u.setTextOrClear(fieldDependsOn, joinList(mergeList(cur.DependsOn, p.AddDeps, p.RemoveDeps)))
	}
	switch {
	case p.ReleaseClaim:
		u.clear(fieldClaim)
		u.clear(fieldClaimExpires)
	case p.Claim != nil && p.Claim.Agent != "":
		u.setText(fieldClaim, p.Claim.Agent)
		u.setText(fieldClaimExpires, p.Claim.Expires.UTC().Format(time.RFC3339))
	}
	return u
}

// applyFields writes each update to the project item. Projects v2 has no bulk
// field mutation, so this is one round trip per changed field.
func (p *Provider) applyFields(ctx context.Context, s *schema, itemID string, updates fieldUpdates) error {
	for _, up := range updates {
		f, ok := s.field(up.Field)
		if !ok {
			return fmt.Errorf("project #%d has no %q field; run `yakanban init --repair` to recreate the yakanban fields",
				s.Number, up.Field)
		}
		vars := map[string]any{"project": s.ProjectID, "item": itemID, "field": f.ID}
		if up.Kind == updateClear {
			if err := p.client.graphql(ctx, clearFieldMutation, vars, nil); err != nil {
				return fmt.Errorf("clearing %s: %w", up.Field, err)
			}
			continue
		}
		value, err := fieldValueFor(f, up)
		if err != nil {
			return err
		}
		vars["value"] = value
		if err := p.client.graphql(ctx, updateFieldMutation, vars, nil); err != nil {
			return fmt.Errorf("setting %s: %w", up.Field, err)
		}
	}
	return nil
}

// fieldValueFor builds the ProjectV2FieldValue input for one update, checking
// the value against the project's own vocabulary for single-select fields.
func fieldValueFor(f field, up fieldUpdate) (map[string]any, error) {
	switch up.Kind {
	case updateSelect:
		if f.DataType != typeSingleSelect {
			return map[string]any{"text": up.Text}, nil
		}
		id, ok := f.optionID(up.Text)
		if !ok {
			return nil, &core.InvalidValueError{Field: strings.ToLower(f.Name), Value: up.Text, Allowed: f.optionNames()}
		}
		return map[string]any{"singleSelectOptionId": id}, nil
	case updateDate:
		return map[string]any{"date": up.Date.Format(dateLayout)}, nil
	default:
		return map[string]any{"text": up.Text}, nil
	}
}
