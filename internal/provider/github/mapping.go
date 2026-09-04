package github

import (
	"strconv"
	"strings"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

// dateLayout is the Projects v2 Date scalar format.
const dateLayout = "2006-01-02"

type labelNode struct {
	Name string `json:"name"`
}

type userNode struct {
	Login string `json:"login"`
}

// issueContent is the Issue half of a project item.
type issueContent struct {
	TypeName  string    `json:"__typename"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	URL       string    `json:"url"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Labels    struct {
		Nodes []labelNode `json:"nodes"`
	} `json:"labels"`
	Assignees struct {
		Nodes []userNode `json:"nodes"`
	} `json:"assignees"`
}

// fieldValue is one project field value, whatever its type.
type fieldValue struct {
	TypeName string   `json:"__typename"`
	Text     *string  `json:"text"`
	Date     *string  `json:"date"`
	Number   *float64 `json:"number"`
	Name     *string  `json:"name"`
	Field    struct {
		Name string `json:"name"`
	} `json:"field"`
}

type fieldValues struct {
	Nodes []fieldValue `json:"nodes"`
}

// byName indexes field values by field name, lowercased.
func (fv fieldValues) byName() map[string]fieldValue {
	out := make(map[string]fieldValue, len(fv.Nodes))
	for _, v := range fv.Nodes {
		if v.Field.Name == "" {
			continue
		}
		out[strings.ToLower(v.Field.Name)] = v
	}
	return out
}

func (v fieldValue) str() string {
	switch {
	case v.Text != nil:
		return strings.TrimSpace(*v.Text)
	case v.Name != nil:
		return strings.TrimSpace(*v.Name)
	case v.Date != nil:
		return strings.TrimSpace(*v.Date)
	case v.Number != nil:
		return strconv.FormatFloat(*v.Number, 'f', -1, 64)
	default:
		return ""
	}
}

type itemNode struct {
	ID          string       `json:"id"`
	IsArchived  bool         `json:"isArchived"`
	Content     issueContent `json:"content"`
	FieldValues fieldValues  `json:"fieldValues"`
}

// toTask projects a GitHub issue plus its project field values onto the
// domain model. Anything GitHub-specific that has no domain field (the
// project item ID, the issue open/closed state) lands in Metadata.
func (n itemNode) toTask() core.Task {
	c := n.Content
	values := n.FieldValues.byName()
	get := func(name string) string { return values[strings.ToLower(name)].str() }

	t := core.Task{
		ID:        strconv.Itoa(c.Number),
		Title:     c.Title,
		Body:      c.Body,
		Status:    get(fieldStatus),
		Priority:  get(fieldPriority),
		Class:     get(fieldClass),
		Estimate:  get(fieldEstimate),
		Blocked:   get(fieldBlocked),
		Parent:    strings.TrimSpace(get(fieldParent)),
		DependsOn: splitList(get(fieldDependsOn)),
		Created:   c.CreatedAt,
		Updated:   c.UpdatedAt,
		URL:       c.URL,
		Due:       parseDate(get(fieldDue)),
		Started:   parseDate(get(fieldStarted)),
		Completed: parseDate(get(fieldCompleted)),
	}
	for _, l := range c.Labels.Nodes {
		t.Tags = append(t.Tags, l.Name)
	}
	for _, a := range c.Assignees.Nodes {
		t.Assignees = append(t.Assignees, a.Login)
	}
	if agent := get(fieldClaim); agent != "" {
		claim := &core.Claim{Agent: agent}
		if exp, err := time.Parse(time.RFC3339, get(fieldClaimExpires)); err == nil {
			claim.Expires = exp
		}
		t.Claim = claim
	}
	t.Metadata = map[string]any{
		"item_id": n.ID,
		"number":  c.Number,
		"state":   strings.ToLower(c.State),
	}
	if n.IsArchived {
		t.Metadata["archived"] = true
	}
	return t
}

// itemID returns the project item node ID stored in a task's metadata.
func itemID(t *core.Task) string {
	if t == nil || t.Metadata == nil {
		return ""
	}
	s, _ := t.Metadata["item_id"].(string)
	return s
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimPrefix(strings.TrimSpace(p), "#")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func joinList(items []string) string { return strings.Join(items, ",") }

func parseDate(s string) *time.Time {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	for _, layout := range []string{dateLayout, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}

// mergeList applies add/remove sets to a list, preserving order and
// rejecting duplicates case-insensitively.
func mergeList(current, add, remove []string) []string {
	out := make([]string, 0, len(current)+len(add))
	drop := make(map[string]bool, len(remove))
	for _, r := range remove {
		drop[strings.ToLower(strings.TrimSpace(r))] = true
	}
	seen := map[string]bool{}
	for _, list := range [][]string{current, add} {
		for _, v := range list {
			key := strings.ToLower(strings.TrimSpace(v))
			if key == "" || drop[key] || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}
