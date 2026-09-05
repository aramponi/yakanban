package gitlab

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/aramponi/yakanban/internal/core"
)

// Bootstrap adopts one explicitly selected/existing board, or provisions a
// label board when the project has none. It never changes adopted columns.
func (p *Provider) Bootstrap(ctx context.Context, o core.BootstrapOptions) (*core.BoardInfo, error) {
	if p.settings.BoardID == 0 {
		boards, err := pages[board](ctx, p.client, p.projectPath()+"/boards")
		if err != nil {
			return nil, err
		}
		switch len(boards) {
		case 0:
			// Validate before the first write: reserved prefixes cannot be statuses.
			for _, st := range o.Statuses {
				if !st.Initial && !st.Terminal && (strings.HasPrefix(st.Name, "priority::") || strings.HasPrefix(st.Name, "class::") || strings.EqualFold(st.Name, "Open") || strings.EqualFold(st.Name, "Closed")) {
					return nil, fmt.Errorf("%w: reserved GitLab status label %q", core.ErrInvalidInput, st.Name)
				}
			}
			var created board
			if _, err := p.client.request(ctx, "POST", p.projectPath()+"/boards", map[string]any{"name": o.Name}, &created); err != nil {
				return nil, err
			}
			if created.ID <= 0 {
				return nil, fmt.Errorf("GitLab created a board without returning an ID; inspect boards before retrying")
			}
			p.settings.BoardID = created.ID
			for _, st := range o.Statuses {
				if st.Initial || st.Terminal {
					continue
				}
				l, err := p.ensureLabel(ctx, st.Name, nil)
				if err != nil {
					return nil, p.bootstrapError(err)
				}
				var list boardList
				if _, err := p.client.request(ctx, "POST", p.projectPath()+"/boards/"+strconv.Itoa(created.ID)+"/lists", map[string]any{"label_id": l.ID}, &list); err != nil {
					return nil, p.bootstrapError(err)
				}
				if list.ID <= 0 || list.Label == nil || list.Label.ID != l.ID {
					return nil, p.bootstrapError(fmt.Errorf("GitLab returned an invalid board list"))
				}
			}
		case 1:
			p.settings.BoardID = boards[0].ID
		default:
			return nil, fmt.Errorf("%w: GitLab project has several boards; select one with --set board_id=ID", core.ErrInvalidInput)
		}
	}
	// Validate scope/list types before adding any vocabulary to an adopted board.
	p.schema = nil
	if _, err := p.load(ctx); err != nil {
		return nil, err
	}
	for i, name := range o.Priorities {
		rank := len(o.Priorities) - 1 - i
		if _, err := p.ensureLabel(ctx, "priority::"+name, &rank); err != nil {
			return nil, p.bootstrapError(err)
		}
	}
	for _, class := range o.Classes {
		if _, err := p.ensureLabel(ctx, "class::"+class.Name, nil); err != nil {
			return nil, p.bootstrapError(err)
		}
	}
	if err := p.store.Invalidate(); err != nil {
		return nil, err
	}
	p.schema = nil
	return p.Board(ctx)
}

func (p *Provider) bootstrapError(err error) error {
	return fmt.Errorf("GitLab board %d exists but setup is incomplete; adopt it with --set board_id=%d: %w", p.settings.BoardID, p.settings.BoardID, err)
}

func (p *Provider) ensureLabel(ctx context.Context, name string, priority *int) (label, error) {
	if strings.TrimSpace(name) == "" || strings.ContainsAny(name, ",\r\n") {
		return label{}, fmt.Errorf("%w: invalid GitLab workflow label %q", core.ErrInvalidInput, name)
	}
	labels, err := pages[label](ctx, p.client, p.projectPath()+"/labels")
	if err != nil {
		return label{}, err
	}
	for _, l := range labels {
		if l.Name == name {
			if l.Archived {
				return label{}, fmt.Errorf("%w: GitLab label %q is archived", core.ErrInvalidInput, name)
			}
			return l, nil
		}
	}
	body := map[string]any{"name": name, "color": "#428BCA"}
	if priority != nil {
		body["priority"] = *priority
	}
	var created label
	if _, err := p.client.request(ctx, "POST", p.projectPath()+"/labels", body, &created); err != nil {
		return label{}, err
	}
	if created.ID <= 0 || created.Name != name {
		return label{}, fmt.Errorf("GitLab label create returned an invalid payload")
	}
	return created, nil
}
