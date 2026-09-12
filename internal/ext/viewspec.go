package ext

import "encoding/json"

// ViewSpec is a declarative description of a piece of UI.
//
// A UI plugin returns ViewSpec JSON; the host renders it with Fyne. Widgets
// never cross the ABI, because a *fyne.CanvasObject cannot be serialized --
// so without this indirection UI plugins would be in-process only and the
// single contract would fracture on its first Kind.
//
// The vocabulary is closed and small on purpose. A Type the host does not know
// renders as a visible placeholder instead of crashing the window, so a plugin
// newer than the host degrades rather than breaks.
type ViewSpec struct {
	// Type is one of the View* constants.
	Type string `json:"type"`
	// ID identifies the node in events it emits. Required for interactive nodes.
	ID string `json:"id,omitempty"`
	// Props carries the type-specific payload.
	Props json.RawMessage `json:"props,omitempty"`
	// Children are nested nodes, for container types.
	Children []ViewSpec `json:"children,omitempty"`
}

// The closed ViewSpec vocabulary.
const (
	ViewStack        = "stack"
	ViewMarkdown     = "markdown"
	ViewAnalogyTable = "analogy_table"
	ViewQuestion     = "question"
	ViewMastery      = "mastery"
)

// StackProps configures a container.
type StackProps struct {
	// Dir is "v" (default) or "h".
	Dir string `json:"dir,omitempty"`
}

// MarkdownProps is prose.
type MarkdownProps struct {
	Text string `json:"text"`
}

// AnalogyRow is one source-to-target pairing.
type AnalogyRow struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	SharedRole string `json:"shared_role,omitempty"`
	CarryOver  string `json:"carry_over,omitempty"`
	// Breakdown says where the analogy stops holding. The renderer draws it
	// with emphasis because it is the part that does the teaching.
	Breakdown string `json:"breakdown"`
}

// AnalogyTableProps is the analogy table.
type AnalogyTableProps struct {
	Rows []AnalogyRow `json:"rows"`
}

// QuestionProps asks the learner something.
type QuestionProps struct {
	// Level is the ladder rung: "L1".."L4".
	Level  string `json:"level,omitempty"`
	Prompt string `json:"prompt"`
	// Options, when set, renders choices instead of a free-text field.
	Options []string `json:"options,omitempty"`
}

// MasteryItem is one concept's progress bar.
type MasteryItem struct {
	Label string  `json:"label"`
	Level float64 `json:"level"`
	Debt  float64 `json:"debt,omitempty"`
}

// MasteryProps is the progress panel.
type MasteryProps struct {
	Items []MasteryItem `json:"items"`
}

// ViewEvent is what the host sends back to a UI plugin when the learner
// interacts with a node.
type ViewEvent struct {
	NodeID  string          `json:"node_id"`
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// View builds a ViewSpec node, encoding props. It is the constructor plugin
// authors use.
func View(typ, id string, props any, children ...ViewSpec) ViewSpec {
	v := ViewSpec{Type: typ, ID: id, Children: children}
	if props != nil {
		if raw, err := json.Marshal(props); err == nil {
			v.Props = raw
		}
	}
	return v
}

// Markdown is shorthand for a prose node.
func Markdown(text string) ViewSpec {
	return View(ViewMarkdown, "", MarkdownProps{Text: text})
}

// Stack is shorthand for a vertical container.
func Stack(children ...ViewSpec) ViewSpec {
	return View(ViewStack, "", StackProps{Dir: "v"}, children...)
}

// DecodeProps decodes a node's props into v. A node with no props decodes as
// the zero value rather than an error.
func (v ViewSpec) DecodeProps(out any) error {
	if len(v.Props) == 0 {
		return nil
	}
	return json.Unmarshal(v.Props, out)
}
